package apikeys

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
	"gorm.io/gorm"
)

// RevokeAPIKeyRequest scopes the revoke to one product; an absent or empty
// audience means no scope.
type RevokeAPIKeyRequest struct {
	Audience string `json:"audience"`
}

type RevokeAPIKeyResponse struct {
	APIKey models.APIKey `json:"api_key"`
}

// RevokeAPIKey handles POST /v1/api-keys/:id/revoke (service key). The body is
// optional. Revoking is idempotent: the original revoked_at is kept.
// Errors: 404 api key not found (unknown id, non-UUID id or different
// audience) · 400 invalid request (malformed JSON) · 401 from ServiceKeyAuth.
func RevokeAPIKey(c *gin.Context) {
	var req RevokeAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	apiKey, ok := changeScopedAPIKey(c, req.Audience, "revoke", func(tx *gorm.DB, id string) error {
		return tx.Model(&models.APIKey{}).
			Where("id = ? AND revoked_at IS NULL", id).
			Update("revoked_at", time.Now()).Error
	})
	if !ok {
		return
	}

	logger.Info("API key revoked: %s (audience %s)", apiKey.ID, apiKey.Audience)
	c.JSON(http.StatusOK, RevokeAPIKeyResponse{APIKey: apiKey})
}
