package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/provider"
	"github.com/fleveque/logo-service/internal/service"
	"github.com/fleveque/logo-service/internal/storage"
)

// AdminHandler handles administrative endpoints.
type AdminHandler struct {
	logoRepo    storage.LogoRepository
	llmCallRepo storage.LLMCallRepository
	ghProvider  *provider.GitHubProvider
	logoService *service.LogoService
	logger      *zap.Logger
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(
	logoRepo storage.LogoRepository,
	llmCallRepo storage.LLMCallRepository,
	ghProvider *provider.GitHubProvider,
	logoService *service.LogoService,
	logger *zap.Logger,
) *AdminHandler {
	return &AdminHandler{
		logoRepo:    logoRepo,
		llmCallRepo: llmCallRepo,
		ghProvider:  ghProvider,
		logoService: logoService,
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

// Import triggers a bulk logo import in a background goroutine.
// Returns 202 Accepted immediately — the import runs asynchronously.
// Route: POST /api/v1/admin/import?source=all
func (h *AdminHandler) Import(c *gin.Context) {
	source := c.DefaultQuery("source", "all")

	if source != "all" && source != "github" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source: must be 'all' or 'github'"})
		return
	}

	// Launch import in background goroutine.
	// Use context.Background() — the HTTP request context gets cancelled
	// when we send the 202 response, but the import should keep running.
	go func() {
		h.logger.Info("starting background import", zap.String("source", source))

		// The callback delegates to LogoService.ProcessAndStore, which handles
		// the full create-record → resize → mark-processed pipeline.
		// This keeps the import logic DRY with the on-demand pipeline.
		callback := func(result *provider.LogoResult) error {
			return h.logoService.ProcessAndStore(context.Background(), result)
		}

		stats, err := h.ghProvider.BulkImport(context.Background(), callback)
		if err != nil {
			h.logger.Error("import failed", zap.Error(err))
			return
		}

		h.logger.Info("import complete",
			zap.Int("total", stats.Total),
			zap.Int("imported", stats.Imported),
			zap.Int("skipped", stats.Skipped),
			zap.Int("failed", stats.Failed),
		)
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"status":  "accepted",
		"source":  source,
		"message": "import started in background",
	})
}
