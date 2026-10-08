package health

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
)

func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/health", HealthCheck)
}

func HealthCheck(c *gin.Context) {
	cfg := config.Get()

	dbStatus := "ok"
	if err := db.HealthCheck(); err != nil {
		dbStatus = "error"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"version":  cfg.Version,
		"database": dbStatus,
	})
}
