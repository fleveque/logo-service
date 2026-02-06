// Package server configures the HTTP server and routes.
package server

import (
	"github.com/gin-gonic/gin"

	"github.com/fleveque/logo-service/internal/handler"
)

// RegisterRoutes sets up all HTTP routes on the Gin engine.
// In Go, we pass dependencies explicitly rather than using DI containers.
// This function will grow as we add more handlers in later phases.
func RegisterRoutes(r *gin.Engine) {
	healthHandler := handler.NewHealthHandler()

	// Public endpoints (no auth)
	r.GET("/healthz", healthHandler.Healthz)

	// API v1 group — auth middleware will be added in Phase 4
	// gin.Group creates a route group that shares a common prefix and middleware.
	// _ = r.Group("/api/v1") // will be used in later phases
}
