// Package apikeys manages per-product API keys: only the SHA-256 hash is
// stored, and every route requires a service key.
package apikeys

import (
	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/middleware"
)

func RegisterRoutes(r *gin.RouterGroup) {
	apiKeys := r.Group("/api-keys", middleware.ServiceKeyAuth())
	apiKeys.POST("", CreateAPIKey)
	apiKeys.GET("", ListAPIKeys)
	apiKeys.POST("/verify", VerifyAPIKey)
	apiKeys.POST("/:id/revoke", RevokeAPIKey)
	apiKeys.PATCH("/:id", UpdateAPIKey)
}
