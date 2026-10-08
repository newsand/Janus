package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/features/auth"
	"github.com/newsand/janus/features/health"
	"github.com/newsand/janus/features/password"
	"github.com/newsand/janus/features/twofa"
	"github.com/newsand/janus/features/users"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
)

func main() {
	cfg := config.Load()

	logger.Init(cfg.LogLevel)
	if err := cfg.Validate(); err != nil {
		logger.Fatal("Invalid configuration: %v", err)
	}
	if cfg.DevEnv {
		logger.Warn("DEV_ENV=true: secret strength checks are disabled — never use in production")
	}
	logger.Info("Starting LoginBuskar Identity Service")
	logger.Info("Version: %s", cfg.Version)

	if err := db.Init(cfg.DatabaseURL); err != nil {
		logger.Fatal("Failed to connect to database: %v", err)
	}
	defer db.Close()

	gin.ForceConsoleColor()
	if cfg.LogLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger())

	v1 := r.Group("/v1")
	{
		health.RegisterRoutes(v1)
		auth.RegisterRoutes(v1)
		users.RegisterRoutes(v1)
		password.RegisterRoutes(v1)
		twofa.RegisterRoutes(v1)
	}

	health.RegisterRoutes(&r.RouterGroup)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("Server listening on :%s", cfg.Port)
		if err := r.Run(":" + cfg.Port); err != nil {
			logger.Fatal("Failed to start server: %v", err)
		}
	}()

	<-quit
	logger.Info("Shutting down server...")
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		logger.Debug("%s %s %d", c.Request.Method, c.Request.URL.Path, c.Writer.Status())
	}
}
