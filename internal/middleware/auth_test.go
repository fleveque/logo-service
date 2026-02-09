package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Go HTTP testing: httptest.NewRecorder() captures the response without starting
// a real server. Combined with gin's test mode, this lets you test handlers
// and middleware in isolation — fast and without network I/O.

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAPIKeyAuth_ValidHeader(t *testing.T) {
	router := gin.New()
	router.Use(APIKeyAuth([]string{"test-key-1", "test-key-2"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "test-key-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPIKeyAuth_ValidQueryParam(t *testing.T) {
	router := gin.New()
	router.Use(APIKeyAuth([]string{"test-key"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test?api_key=test-key", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPIKeyAuth_Missing(t *testing.T) {
	router := gin.New()
	router.Use(APIKeyAuth([]string{"test-key"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAPIKeyAuth_Invalid(t *testing.T) {
	router := gin.New()
	router.Use(APIKeyAuth([]string{"test-key"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminKeyAuth_Valid(t *testing.T) {
	router := gin.New()
	router.Use(AdminKeyAuth([]string{"admin-key"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "admin-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAdminKeyAuth_Invalid(t *testing.T) {
	router := gin.New()
	router.Use(AdminKeyAuth([]string{"admin-key"}))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "not-admin")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}
