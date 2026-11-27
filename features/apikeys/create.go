package apikeys

import (
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
	"gorm.io/gorm"
)

const (
	rawKeyPrefix        = "jns_"
	defaultAudienceType = "text"
	maxNameLength       = 100
	minExpiresInDays    = 1
	maxExpiresInDays    = 365
)

var errInvalidOwner = errors.New("invalid owner")

// CreateAPIKeyRequest types expires_in_days as a float so that a non-integer
// value reaches the range check instead of failing the JSON binding.
type CreateAPIKeyRequest struct {
	OwnerUserID   string   `json:"owner_user_id" binding:"required"`
	Audience      string   `json:"audience" binding:"required,max=100"`
	AudienceType  *string  `json:"audience_type"`
	ExpiresInDays *float64 `json:"expires_in_days"`
	Name          *string  `json:"name"`
}

type CreateAPIKeyResponse struct {
	Key    string        `json:"key"`
	APIKey models.APIKey `json:"api_key"`
}

// CreateAPIKey handles POST /v1/api-keys (service key). The raw key is
// returned once and only its SHA-256 hash is stored.
// A non-UUID owner_user_id is rejected before any query because Postgres
// would fail the uuid cast with a 500.
// Errors: 400 invalid request · 400 invalid owner (non-UUID) ·
// 400 invalid audience_type · 400 expires_in_days must be between 1 and 365 ·
// 400 invalid owner (unknown or disabled) · 401 from ServiceKeyAuth.
func CreateAPIKey(c *gin.Context) {
	var req CreateAPIKeyRequest
	if msg := bindCreateAPIKeyRequest(c, &req); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	rawKey := rawKeyPrefix + models.GenerateOpaqueToken()
	apiKey := models.APIKey{
		OwnerUserID:  req.OwnerUserID,
		Name:         req.Name,
		Audience:     req.Audience,
		AudienceType: defaultAudienceType,
		KeyHash:      models.HashToken(rawKey),
		ExpiresAt:    expiresAt(req.ExpiresInDays),
	}

	err := db.DB().Transaction(func(tx *gorm.DB) error {
		var owner models.User
		if err := tx.First(&owner, "id = ?", req.OwnerUserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errInvalidOwner
			}
			return err
		}
		if owner.DisabledAt != nil {
			return errInvalidOwner
		}
		return tx.Create(&apiKey).Error
	})
	if errors.Is(err, errInvalidOwner) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid owner"})
		return
	}
	if err != nil {
		logger.Error("Failed to create api key for owner %s: %v", req.OwnerUserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	logger.Info("API key created: %s (owner %s, audience %s)", apiKey.ID, apiKey.OwnerUserID, apiKey.Audience)
	c.JSON(http.StatusCreated, CreateAPIKeyResponse{Key: rawKey, APIKey: apiKey})
}

// bindCreateAPIKeyRequest returns the 400 error message of the first failed
// check, or an empty string when the request is valid.
func bindCreateAPIKeyRequest(c *gin.Context, req *CreateAPIKeyRequest) string {
	if err := c.ShouldBindJSON(req); err != nil || (req.Name != nil && len([]rune(*req.Name)) > maxNameLength) {
		return "invalid request"
	}
	if _, err := uuid.Parse(req.OwnerUserID); err != nil {
		return "invalid owner"
	}
	if req.AudienceType != nil && *req.AudienceType != defaultAudienceType {
		return "invalid audience_type"
	}
	if !validExpiresInDays(req.ExpiresInDays) {
		return "expires_in_days must be between 1 and 365"
	}
	return ""
}

func validExpiresInDays(days *float64) bool {
	if days == nil {
		return true
	}
	v := *days
	return v == math.Trunc(v) && v >= minExpiresInDays && v <= maxExpiresInDays
}

func expiresAt(days *float64) *time.Time {
	if days == nil {
		return nil
	}
	t := time.Now().Add(time.Duration(*days) * 24 * time.Hour)
	return &t
}
