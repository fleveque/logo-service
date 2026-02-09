// Package middleware contains Gin middleware functions.
// Middleware in Gin is a handler that runs before (or after) your route handler.
// It calls c.Next() to proceed or c.Abort() to stop the chain.
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIKeyAuth returns middleware that validates API keys.
// The key can be provided via X-API-Key header or api_key query param
// (query param is needed for <img src="...?api_key=xxx"> usage in browsers).
//
// Go closures: this function returns a function. The outer function captures
// `validKeys` in its closure — the returned handler has access to it.
func APIKeyAuth(validKeys []string) gin.HandlerFunc {
	// Build a set for O(1) lookups. Go doesn't have a built-in Set type,
	// so we use map[string]struct{} — struct{} takes zero bytes of memory.
	keySet := make(map[string]struct{}, len(validKeys))
	for _, k := range validKeys {
		keySet[k] = struct{}{}
	}

	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			key = c.Query("api_key")
		}

		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing API key",
			})
			return
		}

		if _, ok := keySet[key]; !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid API key",
			})
			return
		}

		// Store the key in the context for downstream handlers (e.g., rate limiting).
		// gin.Context is like a request-scoped key-value store.
		c.Set("api_key", key)
		c.Next()
	}
}

// AdminKeyAuth returns middleware that validates admin API keys.
// Same pattern as APIKeyAuth but for admin-only endpoints.
func AdminKeyAuth(adminKeys []string) gin.HandlerFunc {
	keySet := make(map[string]struct{}, len(adminKeys))
	for _, k := range adminKeys {
		keySet[k] = struct{}{}
	}

	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			key = c.Query("api_key")
		}

		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing admin API key",
			})
			return
		}

		if _, ok := keySet[key]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "invalid admin API key",
			})
			return
		}

		c.Set("api_key", key)
		c.Next()
	}
}
