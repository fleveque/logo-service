package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS returns middleware that sets Cross-Origin Resource Sharing headers.
// This allows the dividend-portfolio frontend (different origin) to make
// requests to the logo service API.
//
// CORS explained: browsers block cross-origin requests by default. The server
// must explicitly allow them via these headers. For preflight OPTIONS requests,
// we return 204 immediately (no content).
func CORS(allowedOrigins []string) gin.HandlerFunc {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if _, ok := originSet[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "X-API-Key, Content-Type")
			c.Header("Access-Control-Max-Age", "86400")
		}

		// Handle preflight requests
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
