package handler

import (
	"context"
	"fmt"
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
	processor   *service.ImageProcessor
	logger      *zap.Logger
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(
	logoRepo storage.LogoRepository,
	llmCallRepo storage.LLMCallRepository,
	ghProvider *provider.GitHubProvider,
	processor *service.ImageProcessor,
	logger *zap.Logger,
) *AdminHandler {
	return &AdminHandler{
		logoRepo:    logoRepo,
		llmCallRepo: llmCallRepo,
		ghProvider:  ghProvider,
		processor:   processor,
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
//
// Go concurrency note: we spawn a goroutine for the long-running import
// and respond immediately. The goroutine runs independently of the HTTP
// request lifecycle — even if the client disconnects, the import continues.
func (h *AdminHandler) Import(c *gin.Context) {
	source := c.DefaultQuery("source", "all")

	if source != "all" && source != "github" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source: must be 'all' or 'github'"})
		return
	}

	// Launch import in background goroutine
	go func() {
		h.logger.Info("starting background import", zap.String("source", source))

		callback := h.makeImportCallback()

		// Use background context — the HTTP request context gets cancelled
		// when we send the 202 response, but the import should keep running.
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

// makeImportCallback returns the callback function used during bulk import.
// Extracting this as a method keeps Import() clean and makes the pipeline reusable.
func (h *AdminHandler) makeImportCallback() func(result *provider.LogoResult) error {
	return func(result *provider.LogoResult) error {
		ctx := h.newBackgroundCtx()

		// Check if already exists and processed
		existing, err := h.logoRepo.GetBySymbol(ctx, result.Symbol)
		if err == nil && existing.Status == model.StatusProcessed {
			return fmt.Errorf("already exists")
		}

		// Create record if new
		if existing == nil {
			logo := &model.Logo{
				Symbol:      result.Symbol,
				CompanyName: result.CompanyName,
				Source:      result.Source,
				OriginalURL: result.OriginalURL,
				Status:      model.StatusPending,
			}
			if err := h.logoRepo.Create(ctx, logo); err != nil {
				return fmt.Errorf("creating record: %w", err)
			}
		}

		// Process image to all sizes
		sizes, err := h.processor.ProcessAll(result.Symbol, result.ImageData)
		if err != nil {
			_ = h.logoRepo.SetStatus(ctx, result.Symbol, model.StatusFailed, err.Error())
			return fmt.Errorf("processing: %w", err)
		}

		// Mark sizes as available
		for size, ok := range sizes {
			if ok {
				if err := h.logoRepo.SetSizeAvailable(ctx, result.Symbol, size); err != nil {
					h.logger.Error("setting size", zap.String("symbol", result.Symbol), zap.Error(err))
				}
			}
		}

		return h.logoRepo.SetStatus(ctx, result.Symbol, model.StatusProcessed, "")
	}
}

// newBackgroundCtx creates a fresh context for background operations.
// We don't use the request context because the HTTP request may have ended
// by the time the background goroutine processes this logo.
func (h *AdminHandler) newBackgroundCtx() context.Context {
	return context.Background()
}
