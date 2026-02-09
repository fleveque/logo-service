// Package service contains the core business logic for the logo pipeline.
// LogoService orchestrates the 3-layer acquisition strategy:
//
//	Layer 1: Cache — check SQLite metadata + filesystem PNG
//	Layer 2: GitHub — try downloading from open-source ticker-logo repos
//	Layer 3: LLM — ask Claude/OpenAI to search the web for the company logo
//
// Once acquired, logos are processed to all sizes and cached permanently.
package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/provider"
	"github.com/fleveque/logo-service/internal/storage"
)

// LogoService is the main entry point for logo retrieval.
// It implements a "try cache first, then acquire" pattern that's common
// in Go services — check the fast path (local cache), fall back to slower
// external calls only when needed.
type LogoService struct {
	logoRepo    storage.LogoRepository
	fs          *storage.FileSystem
	processor   *ImageProcessor
	ghProvider  *provider.GitHubProvider
	llmProvider *provider.LLMProvider // nil if no LLM keys configured
	logger      *zap.Logger
}

// NewLogoService creates a service with all acquisition layers wired up.
// llmProvider can be nil — the service gracefully skips LLM if unconfigured.
func NewLogoService(
	logoRepo storage.LogoRepository,
	fs *storage.FileSystem,
	processor *ImageProcessor,
	ghProvider *provider.GitHubProvider,
	llmProvider *provider.LLMProvider,
	logger *zap.Logger,
) *LogoService {
	return &LogoService{
		logoRepo:    logoRepo,
		fs:          fs,
		processor:   processor,
		ghProvider:  ghProvider,
		llmProvider: llmProvider,
		logger:      logger,
	}
}

// GetLogo returns the PNG bytes for a logo at the requested size.
// This is the main pipeline:
//  1. Check cache (DB + filesystem)
//  2. If not cached, acquire from providers (GitHub → LLM)
//  3. Process to all sizes, store in cache
//  4. Return the requested size
func (s *LogoService) GetLogo(ctx context.Context, symbol string, size model.LogoSize) ([]byte, error) {
	// Layer 1: Cache hit — fast path
	data, err := s.fromCache(ctx, symbol, size)
	if err == nil {
		return data, nil
	}

	// Cache miss — acquire from external providers
	s.logger.Info("cache miss, acquiring logo",
		zap.String("symbol", symbol),
	)

	result, err := s.acquire(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("acquiring logo for %s: %w", symbol, err)
	}

	// Process and cache for future requests
	if err := s.processAndStore(ctx, result); err != nil {
		return nil, fmt.Errorf("processing logo for %s: %w", symbol, err)
	}

	// Read the now-cached size
	return s.fs.Read(symbol, size)
}

// ProcessAndStore takes a provider result and processes it into all sizes.
// Exported so the admin handler can reuse it during bulk imports.
func (s *LogoService) ProcessAndStore(ctx context.Context, result *provider.LogoResult) error {
	return s.processAndStore(ctx, result)
}

// fromCache checks if we already have this logo at the requested size.
func (s *LogoService) fromCache(ctx context.Context, symbol string, size model.LogoSize) ([]byte, error) {
	logo, err := s.logoRepo.GetBySymbol(ctx, symbol)
	if err != nil {
		return nil, err
	}

	if logo.Status != model.StatusProcessed {
		return nil, fmt.Errorf("logo status is %s", logo.Status)
	}

	if !logo.HasSize(size) {
		return nil, fmt.Errorf("size %s not available", size)
	}

	return s.fs.Read(symbol, size)
}

// acquire tries providers in order: GitHub first (free, fast), then LLM (paid, slow).
func (s *LogoService) acquire(ctx context.Context, symbol string) (*provider.LogoResult, error) {
	// Layer 2: GitHub repos
	result, err := s.ghProvider.GetLogo(ctx, symbol)
	if err == nil {
		s.logger.Info("found logo via GitHub",
			zap.String("symbol", symbol),
			zap.String("source", result.Source),
		)
		return result, nil
	}
	s.logger.Debug("GitHub provider miss",
		zap.String("symbol", symbol),
		zap.Error(err),
	)

	// Layer 3: LLM web search
	if s.llmProvider != nil {
		result, err = s.llmProvider.GetLogo(ctx, symbol)
		if err == nil {
			s.logger.Info("found logo via LLM",
				zap.String("symbol", symbol),
				zap.String("source", result.Source),
			)
			return result, nil
		}
		s.logger.Warn("LLM provider miss",
			zap.String("symbol", symbol),
			zap.Error(err),
		)
	}

	return nil, fmt.Errorf("no provider found a logo for %s", symbol)
}

// processAndStore creates the DB record, resizes the image to all sizes,
// and marks it as processed. This is the shared logic used by both the
// on-demand pipeline (GetLogo) and bulk import (admin handler).
func (s *LogoService) processAndStore(ctx context.Context, result *provider.LogoResult) error {
	// Upsert: create if new, skip if already processed
	existing, err := s.logoRepo.GetBySymbol(ctx, result.Symbol)
	if err == nil && existing.Status == model.StatusProcessed {
		return nil // Already done
	}

	if errors.Is(err, storage.ErrNotFound) {
		logo := &model.Logo{
			Symbol:      result.Symbol,
			CompanyName: result.CompanyName,
			Source:      result.Source,
			OriginalURL: result.OriginalURL,
			Status:      model.StatusPending,
		}
		if err := s.logoRepo.Create(ctx, logo); err != nil {
			return fmt.Errorf("creating record: %w", err)
		}
	}

	// Resize to all 5 sizes
	sizes, err := s.processor.ProcessAll(result.Symbol, result.ImageData)
	if err != nil {
		_ = s.logoRepo.SetStatus(ctx, result.Symbol, model.StatusFailed, err.Error())
		return fmt.Errorf("processing: %w", err)
	}

	// Mark each successful size in the DB
	for size, ok := range sizes {
		if ok {
			if err := s.logoRepo.SetSizeAvailable(ctx, result.Symbol, size); err != nil {
				s.logger.Error("setting size available",
					zap.String("symbol", result.Symbol),
					zap.String("size", string(size)),
					zap.Error(err),
				)
			}
		}
	}

	return s.logoRepo.SetStatus(ctx, result.Symbol, model.StatusProcessed, "")
}
