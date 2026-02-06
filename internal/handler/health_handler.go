// Package handler contains HTTP request handlers.
// In Gin, a handler is any function with signature func(*gin.Context).
// No need for controller classes — just functions grouped by file.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler handles health check requests.
type HealthHandler struct{}

// NewHealthHandler creates a new HealthHandler.
// In Go, constructors are just regular functions prefixed with "New".
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Healthz responds with service status. The method receiver (h *HealthHandler)
// is Go's way of attaching methods to a struct — similar to `self` or `this`.
func (h *HealthHandler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "logo-service",
	})
}
