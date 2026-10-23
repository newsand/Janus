package auth

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/mailer"
	"github.com/newsand/janus/internal/middleware"
	"github.com/newsand/janus/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func RegisterRoutes(r *gin.RouterGroup) {
	auth := r.Group("/auth")
	auth.Use(middleware.RateLimit())
	{
		auth.POST("/login", Login)
		auth.POST("/refresh", Refresh)
		auth.POST("/logout", middleware.JWTAuth(), Logout)
		auth.POST("/magic-link", middleware.ServiceKeyAuth(), RequestMagicLink)
		auth.POST("/magic-link/consume", ConsumeMagicLink)
	}

	me := r.Group("/me")
	me.Use(middleware.JWTAuth())
	{
		me.GET("", GetMe)
	}
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type MagicLinkRequest struct {
	Email   string `json:"email" binding:"required,email"`
	Subject string `json:"subject" binding:"required,max=200"`
	Body    string `json:"body" binding:"required,max=10000"`
}

type MagicLinkConsumeRequest struct {
	Token string `json:"token" binding:"required"`
}

// dummyHashCost must match users.BcryptCost (not imported: users' tests import auth).
const dummyHashCost = 12

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// dummyPasswordHash is a bcrypt hash (same cost as real passwords) that no
// password matches, computed once. Login compares against it when there is
// no real hash so every failure path costs the same.
func dummyPasswordHash() string {
	dummyHashOnce.Do(func() {
		h, _ := bcrypt.GenerateFromPassword([]byte(models.GenerateOpaqueToken()), dummyHashCost)
		dummyHash = string(h)
	})
	return dummyHash
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	lockoutKey := "login:" + req.Email
	if middleware.CheckLockout(c.Request.Context(), lockoutKey) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "account temporarily locked"})
		return
	}

	// One authentication failure, one response: unknown email, no password
	// set, wrong password and disabled account all answer 401 "invalid
	// credentials" and always spend exactly one bcrypt comparison (a dummy
	// hash stands in when there is no real one), so neither the body nor the
	// timing says which case it was.
	var user models.User
	found := db.DB().Where("email = ?", req.Email).First(&user).Error == nil

	hash := dummyPasswordHash()
	if found && user.PasswordHash != nil {
		hash = *user.PasswordHash
	}
	passwordErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password))

	if !found || user.PasswordHash == nil || passwordErr != nil {
		middleware.RecordFailedAttempt(lockoutKey)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// Right password on a disabled account: no token, and no lockout change
	// either way (the caller did prove the password), same indistinguishable 401.
	if user.DisabledAt != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	middleware.ClearLockout(lockoutKey)

	if user.TwoFAEnabled && user.TwoFASecret != nil {
		challengeToken, err := createTwoFAChallenge(user.ID)
		if err != nil {
			logger.Error("Failed to create 2FA challenge: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"requires_2fa":    true,
			"challenge_token": challengeToken,
		})
		return
	}

	tokens, err := issueTokens(user.ID)
	if err != nil {
		logger.Error("Failed to issue tokens: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, tokens)
}

type refreshRow struct {
	ID         string     `gorm:"column:id"`
	UserID     string     `gorm:"column:user_id"`
	FamilyID   string     `gorm:"column:family_id"`
	ExpiresAt  time.Time  `gorm:"column:expires_at"`
	RevokedAt  *time.Time `gorm:"column:revoked_at"`
	DisabledAt *time.Time `gorm:"column:disabled_at"`
}

func Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	tokenHash := models.HashToken(req.RefreshToken)
	var (
		tokens   gin.H
		decision string
	)
	err := db.DB().Transaction(func(tx *gorm.DB) error {
		row, found, err := lockRefresh(tx, tokenHash)
		if err != nil {
			return err
		}
		if !found {
			logger.Debug("Refresh token not found")
			decision = "invalid"
			return nil
		}

		if row.RevokedAt != nil {
			logger.Warn("Reuse detection: revoking family %s", row.FamilyID)
			if err := revokeFamily(tx, row.FamilyID); err != nil {
				return err
			}
			decision = "invalid"
			return nil
		}

		if time.Now().After(row.ExpiresAt) {
			decision = "expired"
			return nil
		}

		if row.DisabledAt != nil {
			decision = "invalid"
			return nil
		}

		res := tx.Model(&models.RefreshToken{}).
			Where("id = ? AND revoked_at IS NULL", row.ID).
			Update("revoked_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			logger.Warn("Reuse detection (TOCTOU): revoking family %s", row.FamilyID)
			if err := revokeFamily(tx, row.FamilyID); err != nil {
				return err
			}
			decision = "invalid"
			return nil
		}

		tokens, err = issueTokensOn(tx, row.UserID, row.FamilyID)
		if err != nil {
			return err
		}
		decision = "ok"
		return nil
	})
	if err != nil {
		logger.Error("Failed to rotate refresh token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	switch decision {
	case "ok":
		c.JSON(http.StatusOK, tokens)
	case "expired":
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token expired"})
	default:
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
	}
}

func lockRefresh(tx *gorm.DB, tokenHash string) (refreshRow, bool, error) {
	q := tx.Table("refresh_tokens AS rt").
		Select("rt.id, rt.user_id, rt.family_id, rt.expires_at, rt.revoked_at, u.disabled_at").
		Joins("INNER JOIN users AS u ON u.id = rt.user_id").
		Where("rt.token_hash = ?", tokenHash)
	if tx.Dialector.Name() == "postgres" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "rt"}})
	}
	var rows []refreshRow
	if err := q.Scan(&rows).Error; err != nil {
		return refreshRow{}, false, err
	}
	if len(rows) == 0 {
		return refreshRow{}, false, nil
	}
	return rows[0], true, nil
}

func revokeFamily(tx *gorm.DB, familyID string) error {
	return tx.Model(&models.RefreshToken{}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Update("revoked_at", time.Now()).Error
}

func Logout(c *gin.Context) {
	userID, _ := c.Get("user_id")

	db.DB().Model(&models.RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", time.Now())

	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func GetMe(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var user models.User
	if err := db.DB().First(&user, "id = ?", userID).Error; err != nil {
		logger.Error("Failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          user.ID,
		"email":       user.Email,
		"nome":        user.Nome,
		"2fa_enabled": user.TwoFAEnabled,
	})
}

func RequestMagicLink(c *gin.Context) {
	var req MagicLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if !strings.Contains(req.Body, mailer.LinkPlaceholder) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": mailer.ErrMissingLink.Error()})
		return
	}

	generic := gin.H{"message": "if the email exists, a magic link will be sent"}

	var user models.User
	if err := db.DB().Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, generic)
		return
	}

	if user.DisabledAt != nil || user.PasswordHash == nil {
		c.JSON(http.StatusOK, generic)
		return
	}

	err := db.DB().Transaction(func(p_Tx *gorm.DB) error {
		token, err := createMagicTokenTx(p_Tx, user.ID)
		if err != nil {
			return err
		}
		return mailer.EnqueueMagicLink(p_Tx, user.Email, req.Subject, req.Body, token)
	})
	if errors.Is(err, mailer.ErrMissingLink) || errors.Is(err, mailer.ErrTooLong) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		logger.Error("Failed to enqueue magic link: %v", err)
	}

	c.JSON(http.StatusOK, generic)
}

func ConsumeMagicLink(c *gin.Context) {
	var req MagicLinkConsumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	tokenHash := models.HashToken(req.Token)

	var magic models.MagicToken
	if err := db.DB().Where("token_hash = ?", tokenHash).First(&magic).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	if magic.UsedAt != nil || time.Now().After(magic.ExpiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	now := time.Now()
	db.DB().Model(&magic).Update("used_at", now)

	var user models.User
	if err := db.DB().First(&user, "id = ?", magic.UserID).Error; err != nil || user.DisabledAt != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	if user.TwoFAEnabled && user.TwoFASecret != nil {
		challengeToken, err := createTwoFAChallenge(user.ID)
		if err != nil {
			logger.Error("Failed to create 2FA challenge: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"requires_2fa":    true,
			"challenge_token": challengeToken,
		})
		return
	}

	tokens, err := issueTokens(user.ID)
	if err != nil {
		logger.Error("Failed to issue tokens: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, tokens)
}

func issueTokens(userID string) (gin.H, error) {
	familyID := uuid.New().String()
	return issueTokensWithFamily(userID, familyID)
}

func issueTokensWithFamily(userID, familyID string) (gin.H, error) {
	return issueTokensOn(db.DB(), userID, familyID)
}

func issueTokensOn(tx *gorm.DB, userID, familyID string) (gin.H, error) {
	cfg := config.Get()

	jti := uuid.New().String()
	now := time.Now()

	claims := jwt.MapClaims{
		"sub":          userID,
		"iat":          now.Unix(),
		"exp":          now.Add(cfg.AccessTokenTTL).Unix(),
		"jti":          jti,
		"2fa_verified": true,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return nil, err
	}

	opaqueToken := models.GenerateOpaqueToken()
	refreshToken := models.RefreshToken{
		UserID:    userID,
		TokenHash: models.HashToken(opaqueToken),
		FamilyID:  familyID,
		ExpiresAt: now.Add(cfg.RefreshTokenTTL),
	}

	if err := tx.Create(&refreshToken).Error; err != nil {
		return nil, err
	}

	return gin.H{
		"access_token":  accessToken,
		"refresh_token": opaqueToken,
		"token_type":    "Bearer",
		"expires_in":    int(cfg.AccessTokenTTL.Seconds()),
	}, nil
}

func createMagicTokenTx(p_Tx *gorm.DB, p_UserID string) (string, error) {
	cfg := config.Get()

	if err := p_Tx.Model(&models.MagicToken{}).
		Where("user_id = ? AND used_at IS NULL", p_UserID).
		Update("used_at", time.Now()).Error; err != nil {
		return "", err
	}

	opaqueToken := models.GenerateOpaqueToken()
	magic := models.MagicToken{
		UserID:    p_UserID,
		TokenHash: models.HashToken(opaqueToken),
		ExpiresAt: time.Now().Add(cfg.MagicLinkTTL),
	}

	if err := p_Tx.Create(&magic).Error; err != nil {
		return "", err
	}

	return opaqueToken, nil
}

func createTwoFAChallenge(userID string) (string, error) {
	db.DB().Where("user_id = ?", userID).Delete(&models.TwoFAChallenge{})

	opaqueToken := models.GenerateOpaqueToken()
	challenge := models.TwoFAChallenge{
		UserID:    userID,
		TokenHash: models.HashToken(opaqueToken),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	if err := db.DB().Create(&challenge).Error; err != nil {
		return "", err
	}

	return opaqueToken, nil
}
