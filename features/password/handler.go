package password

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/mailer"
	"github.com/newsand/janus/internal/middleware"
	"github.com/newsand/janus/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const BcryptCost = 12

func RegisterRoutes(r *gin.RouterGroup) {
	pwd := r.Group("/password")
	pwd.Use(middleware.RateLimit())
	{
		pwd.POST("/forgot", middleware.ServiceKeyAuth(), Forgot)
		pwd.POST("/reset", Reset)
	}
}

type ForgotRequest struct {
	Email   string `json:"email" binding:"required,email"`
	Subject string `json:"subject" binding:"required,max=200"`
	Body    string `json:"body" binding:"required,max=10000"`
}

type ResetRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

func Forgot(c *gin.Context) {
	var req ForgotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if !strings.Contains(req.Body, mailer.LinkPlaceholder) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": mailer.ErrMissingLink.Error()})
		return
	}

	generic := gin.H{"message": "if the email exists, a recovery link will be sent"}
	cfg := config.Get()

	var user models.User
	if err := db.DB().Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, generic)
		return
	}

	if user.DisabledAt != nil {
		c.JSON(http.StatusOK, generic)
		return
	}

	opaqueToken := models.GenerateOpaqueToken()
	err := db.DB().Transaction(func(p_Tx *gorm.DB) error {
		if err := p_Tx.Model(&models.RecoverToken{}).
			Where("user_id = ? AND used_at IS NULL", user.ID).
			Update("used_at", time.Now()).Error; err != nil {
			return err
		}

		recoverToken := models.RecoverToken{
			UserID:    user.ID,
			TokenHash: models.HashToken(opaqueToken),
			ExpiresAt: time.Now().Add(cfg.RecoverTTL),
		}
		if err := p_Tx.Create(&recoverToken).Error; err != nil {
			return err
		}
		return mailer.EnqueueRecover(p_Tx, req.Email, req.Subject, req.Body, opaqueToken)
	})
	if errors.Is(err, mailer.ErrMissingLink) || errors.Is(err, mailer.ErrTooLong) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		logger.Error("Failed to enqueue recovery email: %v", err)
	}

	c.JSON(http.StatusOK, generic)
}

func Reset(c *gin.Context) {
	var req ResetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
		return
	}

	tokenHash := models.HashToken(req.Token)

	var recoverToken models.RecoverToken
	if err := db.DB().Where("token_hash = ?", tokenHash).First(&recoverToken).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	if recoverToken.UsedAt != nil || time.Now().After(recoverToken.ExpiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), BcryptCost)
	if err != nil {
		logger.Error("Failed to hash password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	passwordHash := string(hash)
	db.DB().Model(&models.User{}).Where("id = ?", recoverToken.UserID).Update("password_hash", passwordHash)

	db.DB().Model(&recoverToken).Update("used_at", time.Now())

	db.DB().Model(&models.RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", recoverToken.UserID).
		Update("revoked_at", time.Now())

	logger.Info("Password reset completed for user: %s", recoverToken.UserID)
	c.JSON(http.StatusOK, gin.H{"message": "password reset successful"})
}
