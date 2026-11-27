package apikeys

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
	"gorm.io/gorm"
)

var errAPIKeyNotFound = errors.New("api key not found")

// apiKeyIDParam returns the :id path param. A non-UUID id answers 404, never
// 400, so callers cannot tell a malformed id from an unknown one.
func apiKeyIDParam(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
		c.JSON(http.StatusNotFound, gin.H{"error": errAPIKeyNotFound.Error()})
		return "", false
	}
	return id, true
}

// mutateScopedAPIKey runs mutate in one transaction after an audience-scoped
// lookup (empty audience means no scope) and returns the reloaded row. A key
// outside the scope yields errAPIKeyNotFound.
func mutateScopedAPIKey(id, audience string, mutate func(tx *gorm.DB) error) (models.APIKey, error) {
	var apiKey models.APIKey
	err := db.DB().Transaction(func(tx *gorm.DB) error {
		query := tx.Where("id = ?", id)
		if audience != "" {
			query = query.Where("audience = ?", audience)
		}
		if err := query.First(&apiKey).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errAPIKeyNotFound
			}
			return err
		}
		if err := mutate(tx); err != nil {
			return err
		}
		return tx.First(&apiKey, "id = ?", id).Error
	})
	return apiKey, err
}

// changeScopedAPIKey validates the :id param, applies change through
// mutateScopedAPIKey and writes the 404/500 response on failure. It reports
// whether the handler may answer with the returned key.
func changeScopedAPIKey(c *gin.Context, audience, action string, change func(tx *gorm.DB, id string) error) (models.APIKey, bool) {
	id, ok := apiKeyIDParam(c)
	if !ok {
		return models.APIKey{}, false
	}
	apiKey, err := mutateScopedAPIKey(id, audience, func(tx *gorm.DB) error { return change(tx, id) })
	if respondAPIKeyMutationError(c, err, action, id) {
		return models.APIKey{}, false
	}
	return apiKey, true
}

// respondAPIKeyMutationError maps a mutateScopedAPIKey error to 404 or 500 and
// reports whether a response was written.
func respondAPIKeyMutationError(c *gin.Context, err error, action, id string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errAPIKeyNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": errAPIKeyNotFound.Error()})
		return true
	}
	logger.Error("Failed to %s api key %s: %v", action, id, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	return true
}
