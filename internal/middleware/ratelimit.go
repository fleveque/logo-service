package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimit returns per-API-key rate limiting middleware using token buckets.
//
// Token bucket algorithm: each key gets a bucket that fills at `rps` tokens/sec
// up to `burst` tokens. Each request consumes one token. If the bucket is empty,
// the request is rejected with 429.
//
// sync.Mutex protects the map of limiters from concurrent goroutine access.
// This is one of the few cases where Go uses traditional locks instead of channels —
// a shared map with simple read/write is cleaner with a mutex than a channel.
func RateLimit(rps float64, burst int) gin.HandlerFunc {
	var mu sync.Mutex
	limiters := make(map[string]*rate.Limiter)

	return func(c *gin.Context) {
		// Get the API key set by auth middleware
		key, exists := c.Get("api_key")
		if !exists {
			// No API key means auth middleware didn't run — allow through
			c.Next()
			return
		}

		apiKey := key.(string) // Type assertion: interface{} → string

		mu.Lock()
		limiter, exists := limiters[apiKey]
		if !exists {
			limiter = rate.NewLimiter(rate.Limit(rps), burst)
			limiters[apiKey] = limiter
		}
		mu.Unlock()

		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}
