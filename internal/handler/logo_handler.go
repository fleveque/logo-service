package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/service"
)

// LogoHandler handles requests for logo images.
// It delegates to LogoService which implements the full 3-layer pipeline:
// cache → GitHub → LLM.
type LogoHandler struct {
	logoService *service.LogoService
	logger      *zap.Logger
}

// NewLogoHandler creates a new LogoHandler with the logo service.
func NewLogoHandler(logoService *service.LogoService, logger *zap.Logger) *LogoHandler {
	return &LogoHandler{
		logoService: logoService,
		logger:      logger,
	}
}

// GetLogo serves a logo image for the given stock symbol.
// Route: GET /api/v1/logos/:symbol?size=m&bg=ffffff
//
// If the logo isn't cached, the service transparently acquires it from
// GitHub repos or via LLM web search, processes it, and caches it.
func (h *LogoHandler) GetLogo(c *gin.Context) {
	symbol := strings.ToUpper(c.Param("symbol"))

	// Validate size parameter
	sizeStr := c.DefaultQuery("size", "m")
	if !model.ValidSize(sizeStr) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid size: must be xs, s, m, l, or xl",
		})
		return
	}
	size := model.LogoSize(sizeStr)

	// GetLogo handles the full pipeline: cache → GitHub → LLM → process
	data, err := h.logoService.GetLogo(c.Request.Context(), symbol, size)
	if err != nil {
		h.logger.Warn("logo not found",
			zap.String("symbol", symbol),
			zap.Error(err),
		)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "logo not found",
		})
		return
	}

	// Apply background color if requested
	bgColor := c.Query("bg")
	if bgColor != "" {
		data, err = service.ApplyBackground(data, bgColor)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid background color: " + err.Error(),
			})
			return
		}
	}

	// Set cache headers — logos don't change often
	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, "image/png", data)
}
