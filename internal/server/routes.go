// Package server configures the HTTP server and routes.
package server

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/config"
	"github.com/fleveque/logo-service/internal/handler"
	"github.com/fleveque/logo-service/internal/middleware"
)

// RegisterRoutes sets up all HTTP routes on the Gin engine.
// In Go, we pass dependencies explicitly — no DI container, no magic.
// Each handler gets exactly the dependencies it needs.
func RegisterRoutes(r *gin.Engine, cfg *config.Config, deps Deps, logger *zap.Logger) {
	healthHandler := handler.NewHealthHandler()
	logoHandler := handler.NewLogoHandler(deps.LogoService, logger)
	adminHandler := handler.NewAdminHandler(deps.LogoRepo, deps.LLMCallRepo, deps.GitHubProvider, deps.LogoService, logger)

	// Public endpoints (no auth)
	r.GET("/healthz", healthHandler.Healthz)

	// CORS middleware applies to the entire API group.
	api := r.Group("/api/v1")
	api.Use(middleware.CORS(cfg.CORS.AllowedOrigins))

	// Authenticated API endpoints
	authed := api.Group("")
	authed.Use(middleware.APIKeyAuth(cfg.Auth.APIKeys))
	authed.Use(middleware.RateLimit(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst))
	{
		authed.GET("/logos/:symbol", logoHandler.GetLogo)
	}

	// Admin endpoints (separate auth with admin keys)
	admin := api.Group("/admin")
	admin.Use(middleware.AdminKeyAuth(cfg.Auth.AdminKeys))
	{
		admin.GET("/stats", adminHandler.Stats)
		admin.POST("/import", adminHandler.Import)
	}
}
