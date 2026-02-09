package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRateLimit_AllowsNormalTraffic(t *testing.T) {
	router := gin.New()
	// Set API key first (rate limiter reads it from context)
	router.Use(func(c *gin.Context) {
		c.Set("api_key", "test-key")
		c.Next()
	})
	router.Use(RateLimit(10, 5)) // 10 req/s, burst of 5
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// First 5 requests should succeed (within burst)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestRateLimit_RejectsExcessiveTraffic(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("api_key", "test-key")
		c.Next()
	})
	router.Use(RateLimit(1, 2)) // 1 req/s, burst of 2
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Exhaust the burst
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// Next request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestRateLimit_PerKeyIsolation(t *testing.T) {
	router := gin.New()
	// Dynamic API key from header
	router.Use(func(c *gin.Context) {
		c.Set("api_key", c.GetHeader("X-API-Key"))
		c.Next()
	})
	router.Use(RateLimit(1, 1)) // Very tight: 1 req/s, burst of 1
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Key A uses its burst
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "key-a")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("key-a first request: expected 200, got %d", w.Code)
	}

	// Key A is now rate limited
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "key-a")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("key-a second request: expected 429, got %d", w.Code)
	}

	// Key B should still work (separate bucket)
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "key-b")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("key-b first request: expected 200, got %d", w.Code)
	}
}
