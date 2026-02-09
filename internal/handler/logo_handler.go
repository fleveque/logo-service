package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/service"
	"github.com/fleveque/logo-service/internal/storage"
)

// LogoHandler handles requests for logo images.
type LogoHandler struct {
	logoRepo storage.LogoRepository
	fs       *storage.FileSystem
	logger   *zap.Logger
}

// NewLogoHandler creates a new LogoHandler with its dependencies.
func NewLogoHandler(logoRepo storage.LogoRepository, fs *storage.FileSystem, logger *zap.Logger) *LogoHandler {
	return &LogoHandler{
		logoRepo: logoRepo,
		fs:       fs,
		logger:   logger,
	}
}

// GetLogo serves a logo image for the given stock symbol.
// Route: GET /api/v1/logos/:symbol?size=m&bg=ffffff
//
// Gin extracts URL params with c.Param() and query params with c.DefaultQuery().
// The response is raw PNG bytes with appropriate cache headers.
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

	// Check if logo exists in database
	logo, err := h.logoRepo.GetBySymbol(c.Request.Context(), symbol)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "logo not found",
			})
			return
		}
		h.logger.Error("database error", zap.String("symbol", symbol), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal error",
		})
		return
	}

	// Check if the requested size is available
	if !logo.HasSize(size) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "logo size not available",
		})
		return
	}

	// Read the PNG from disk
	data, err := h.fs.Read(symbol, size)
	if err != nil {
		h.logger.Error("reading logo file",
			zap.String("symbol", symbol),
			zap.String("size", string(size)),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal error",
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
