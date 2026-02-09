package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/storage"
)

// AdminHandler handles administrative endpoints.
type AdminHandler struct {
	logoRepo    storage.LogoRepository
	llmCallRepo storage.LLMCallRepository
	logger      *zap.Logger
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(logoRepo storage.LogoRepository, llmCallRepo storage.LLMCallRepository, logger *zap.Logger) *AdminHandler {
	return &AdminHandler{
		logoRepo:    logoRepo,
		llmCallRepo: llmCallRepo,
		logger:      logger,
	}
}

// Stats returns logo counts and service statistics.
// Route: GET /api/v1/admin/stats
func (h *AdminHandler) Stats(c *gin.Context) {
	ctx := c.Request.Context()

	total, err := h.logoRepo.Count(ctx)
	if err != nil {
		h.logger.Error("counting logos", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	processed, err := h.logoRepo.CountByStatus(ctx, model.StatusProcessed)
	if err != nil {
		h.logger.Error("counting processed logos", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	pending, err := h.logoRepo.CountByStatus(ctx, model.StatusPending)
	if err != nil {
		h.logger.Error("counting pending logos", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	failed, err := h.logoRepo.CountByStatus(ctx, model.StatusFailed)
	if err != nil {
		h.logger.Error("counting failed logos", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total":     total,
		"processed": processed,
		"pending":   pending,
		"failed":    failed,
	})
}

// Import triggers a bulk logo import. Source is specified via query param.
// Route: POST /api/v1/admin/import?source=all
// The actual import logic will be added in Phase 5 (GitHub provider).
func (h *AdminHandler) Import(c *gin.Context) {
	source := c.DefaultQuery("source", "all")

	// Placeholder — will be wired to the import service in Phase 5
	c.JSON(http.StatusAccepted, gin.H{
		"status":  "accepted",
		"source":  source,
		"message": "import not yet implemented",
	})
}
