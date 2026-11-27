package apikeys

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
	"gorm.io/gorm"
)

type VerifyAPIKeyRequest struct {
	Key      *string `json:"key" binding:"required"`
	Audience *string `json:"audience" binding:"required"`
}

type VerifyAPIKeyResponse struct {
	Valid       bool   `json:"valid"`
	KeyID       string `json:"key_id"`
	OwnerUserID string `json:"owner_user_id"`
	Audience    string `json:"audience"`
}

type verifiedAPIKey struct {
	ID          string
	OwnerUserID string
	Audience    string
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	DisabledAt  *time.Time
}

// errInvalidAPIKey is the single 401 for every invalid reason, so callers
// cannot tell unknown, revoked, expired, disabled-owner or wrong-audience apart.
var errInvalidAPIKey = errors.New("invalid api key")

// VerifyAPIKey handles POST /v1/api-keys/verify (service key). Read-only.
// Errors: 401 invalid api key (unknown, no jns_ prefix, revoked, expired,
// owner disabled, audience mismatch — identical body) · 400 invalid request
// (malformed body, key or audience missing) · 401 from ServiceKeyAuth.
func VerifyAPIKey(c *gin.Context) {
	var req VerifyAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	key, err := findValidAPIKey(*req.Key, *req.Audience)
	if errors.Is(err, errInvalidAPIKey) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": errInvalidAPIKey.Error()})
		return
	}
	if err != nil {
		logger.Error("Failed to verify api key: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, VerifyAPIKeyResponse{
		Valid:       true,
		KeyID:       key.ID,
		OwnerUserID: key.OwnerUserID,
		Audience:    key.Audience,
	})
}

func findValidAPIKey(rawKey, audience string) (*verifiedAPIKey, error) {
	if !strings.HasPrefix(rawKey, rawKeyPrefix) {
		return nil, errInvalidAPIKey
	}

	var key verifiedAPIKey
	err := db.DB().Model(&models.APIKey{}).
		Select("api_keys.id, api_keys.owner_user_id, api_keys.audience, api_keys.expires_at, api_keys.revoked_at, users.disabled_at").
		Joins("JOIN users ON users.id = api_keys.owner_user_id").
		Where("api_keys.key_hash = ?", models.HashToken(rawKey)).
		Take(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errInvalidAPIKey
	}
	if err != nil {
		return nil, err
	}

	if key.RevokedAt != nil || key.DisabledAt != nil || key.Audience != audience ||
		(key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
		return nil, errInvalidAPIKey
	}
	return &key, nil
}
