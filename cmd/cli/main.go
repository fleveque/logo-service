// Package main provides the CLI tool for the logo-service.
// Uses Cobra for command parsing — Cobra is the standard Go CLI framework
// (used by kubectl, docker, hugo, and many others).
//
// Run with: go run ./cmd/cli import --source all
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/config"
	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/provider"
	"github.com/fleveque/logo-service/internal/service"
	"github.com/fleveque/logo-service/internal/storage"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// rootCmd creates the root command. Cobra builds a tree of commands:
// logo-cli import --source all
// logo-cli import --source github
func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "logo-cli",
		Short: "Logo service CLI tools",
	}

	root.AddCommand(importCmd())
	return root
}

func importCmd() *cobra.Command {
	var source string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Bulk import logos from external sources",
		// RunE returns an error (vs Run which doesn't). Cobra prints the error automatically.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(source)
		},
	}

	// Cobra flags: --source with default "all"
	cmd.Flags().StringVar(&source, "source", "all", "Import source: all, github")
	return cmd
}

func runImport(source string) error {
	// Load config
	configPath := os.Getenv("LOGO_CONFIG_PATH")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Set up logger (always use development mode for CLI)
	logger, err := zap.NewDevelopment()
	if err != nil {
		return fmt.Errorf("creating logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	// Initialize storage
	if err := os.MkdirAll(filepath.Dir(cfg.Storage.DatabasePath), 0755); err != nil {
		return fmt.Errorf("creating database directory: %w", err)
	}

	db, err := storage.NewDatabase(cfg.Storage.DatabasePath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	fs, err := storage.NewFileSystem(cfg.Storage.LogoDir)
	if err != nil {
		return fmt.Errorf("creating filesystem: %w", err)
	}

	logoRepo := storage.NewLogoRepository(db)
	processor := service.NewImageProcessor(fs)

	// Set up context with cancellation (Ctrl+C to stop import gracefully)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("cancelling import...")
		cancel()
	}()

	// Run import based on source
	switch source {
	case "all", "github":
		return runGitHubImport(ctx, cfg, logoRepo, processor, logger)
	default:
		return fmt.Errorf("unknown source: %s", source)
	}
}

func runGitHubImport(ctx context.Context, cfg *config.Config, logoRepo storage.LogoRepository, processor *service.ImageProcessor, logger *zap.Logger) error {
	ghProvider := provider.NewGitHubProvider(cfg.GitHub.Repos, logger)

	// The callback processes each logo as it's downloaded.
	// This is where the provider → processor → repository pipeline runs.
	callback := func(result *provider.LogoResult) error {
		// Check if already exists
		existing, err := logoRepo.GetBySymbol(ctx, result.Symbol)
		if err == nil && existing.Status == model.StatusProcessed {
			return fmt.Errorf("already exists")
		}

		// Create or update the logo record
		if existing == nil {
			logo := &model.Logo{
				Symbol:      result.Symbol,
				CompanyName: result.CompanyName,
				Source:      result.Source,
				OriginalURL: result.OriginalURL,
				Status:      model.StatusPending,
			}
			if err := logoRepo.Create(ctx, logo); err != nil {
				return fmt.Errorf("creating record: %w", err)
			}
		}

		// Process the image (resize to all sizes)
		sizes, err := processor.ProcessAll(result.Symbol, result.ImageData)
		if err != nil {
			_ = logoRepo.SetStatus(ctx, result.Symbol, model.StatusFailed, err.Error())
			return fmt.Errorf("processing image: %w", err)
		}

		// Mark each size as available
		for size, ok := range sizes {
			if ok {
				if err := logoRepo.SetSizeAvailable(ctx, result.Symbol, size); err != nil {
					logger.Error("setting size available",
						zap.String("symbol", result.Symbol),
						zap.String("size", string(size)),
						zap.Error(err),
					)
				}
			}
		}

		// Mark as processed
		if err := logoRepo.SetStatus(ctx, result.Symbol, model.StatusProcessed, ""); err != nil {
			return fmt.Errorf("setting status: %w", err)
		}

		return nil
	}

	stats, err := ghProvider.BulkImport(ctx, callback)
	if err != nil {
		return fmt.Errorf("bulk import: %w", err)
	}

	logger.Info("import complete",
		zap.Int("total", stats.Total),
		zap.Int("imported", stats.Imported),
		zap.Int("skipped", stats.Skipped),
		zap.Int("failed", stats.Failed),
	)

	if len(stats.Errors) > 0 {
		logger.Warn("import had errors", zap.Int("count", len(stats.Errors)))
	}

	return nil
}
