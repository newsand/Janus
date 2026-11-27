package apikeys

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
	"gorm.io/gorm"
)

// UpdateAPIKeyRequest carries the only editable field, name; audience only
// scopes the lookup. Any other field present in the body is rejected, even
// with a null value.
type UpdateAPIKeyRequest struct {
	Name     *string `json:"name"`
	Audience string  `json:"audience"`
}

type UpdateAPIKeyResponse struct {
	APIKey models.APIKey `json:"api_key"`
}

const UNKNOWN_FIELD_ERR_PREFIX = "json: unknown field "

// UpdateAPIKey handles PATCH /v1/api-keys/:id (service key). It renames the
// key: a blank or null name is stored as NULL. Revoked or expired keys may be
// renamed; key_hash, revoked_at and expires_at are never touched.
// Errors: 400 only name is editable · 400 invalid request (malformed JSON,
// non-string name, name over 100 chars) · 404 api key not found (unknown id,
// non-UUID id or different audience) · 401 from ServiceKeyAuth.
func UpdateAPIKey(c *gin.Context) {
	var req UpdateAPIKeyRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		if strings.HasPrefix(err.Error(), UNKNOWN_FIELD_ERR_PREFIX) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "only name is editable"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Name != nil && len([]rune(*req.Name)) > maxNameLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	name := req.Name
	if name != nil && strings.TrimSpace(*name) == "" {
		name = nil
	}

	apiKey, ok := changeScopedAPIKey(c, req.Audience, "update", func(tx *gorm.DB, id string) error {
		return tx.Model(&models.APIKey{}).Where("id = ?", id).Update("name", name).Error
	})
	if !ok {
		return
	}

	logger.Info("API key renamed: %s (audience %s)", apiKey.ID, apiKey.Audience)
	c.JSON(http.StatusOK, UpdateAPIKeyResponse{APIKey: apiKey})
}
