package apikeys

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
)

type ListAPIKeysQuery struct {
	OwnerUserID string `form:"owner_user_id" binding:"omitempty,uuid"`
	Audience    string `form:"audience"`
}

type ListAPIKeysResponse struct {
	APIKeys []models.APIKey `json:"api_keys"`
}

// ListAPIKeys handles GET /v1/api-keys?owner_user_id=&audience= (service key).
// Filters are optional, exact-match and AND-combined; revoked and expired keys
// are listed too. Order: created_at DESC, id DESC.
// Errors: 400 invalid request (owner_user_id not a UUID) · 401 from ServiceKeyAuth.
func ListAPIKeys(c *gin.Context) {
	var q ListAPIKeysQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	query := db.DB().Model(&models.APIKey{})
	if q.OwnerUserID != "" {
		query = query.Where("owner_user_id = ?", q.OwnerUserID)
	}
	if q.Audience != "" {
		query = query.Where("audience = ?", q.Audience)
	}

	apiKeys := []models.APIKey{}
	if err := query.Order("created_at DESC").Order("id DESC").Find(&apiKeys).Error; err != nil {
		logger.Error("Failed to list api keys: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, ListAPIKeysResponse{APIKeys: apiKeys})
}
