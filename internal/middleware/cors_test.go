package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORS_AllowedOrigin(t *testing.T) {
	router := gin.New()
	router.Use(CORS([]string{"http://localhost:3000", "https://quantic.es"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("expected CORS origin header, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	router := gin.New()
	router.Use(CORS([]string{"http://localhost:3000"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no CORS header for disallowed origin, got %q",
			w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_PreflightOptions(t *testing.T) {
	router := gin.New()
	router.Use(CORS([]string{"http://localhost:3000"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Access-Control-Allow-Methods header")
	}
}
