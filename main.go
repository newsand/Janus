package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/features/auth"
	"github.com/newsand/janus/features/health"
	"github.com/newsand/janus/features/password"
	"github.com/newsand/janus/features/twofa"
	"github.com/newsand/janus/features/users"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/mailer"
	"github.com/newsand/janus/migrations"
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
	logger.Info("Starting Janus Identity Service")
	logger.Info("Version: %s", cfg.Version)

	if err := db.Init(cfg.DatabaseURL); err != nil {
		logger.Fatal("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := migrations.Up(db.DB()); err != nil {
		logger.Fatal("Failed to run migrations: %v", err)
	}
	db.EnablePreparedStatements()

	mailer.Init(cfg)

	gin.ForceConsoleColor()
	if cfg.LogLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(limitRequestBody(1 << 20))
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

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("Server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server: %v", err)
		}
	}()

	<-quit
	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("HTTP shutdown: %v", err)
	}
	mailer.Stop()
}

func limitRequestBody(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		logger.Debug("%s %s %d", c.Request.Method, c.Request.URL.Path, c.Writer.Status())
	}
}
