// Package main is the entry point for the logo-service HTTP server.
// In Go, the `main` package with a `main()` function is what gets executed.
// Unlike Ruby/JS, Go compiles to a single static binary — no runtime needed.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/config"
	"github.com/fleveque/logo-service/internal/llm"
	"github.com/fleveque/logo-service/internal/provider"
	"github.com/fleveque/logo-service/internal/server"
	"github.com/fleveque/logo-service/internal/service"
	"github.com/fleveque/logo-service/internal/storage"
)

func main() {
	// os.Exit ensures the process exits with a non-zero code on failure.
	// We call run() separately so deferred cleanup functions execute properly
	// (deferred functions don't run when os.Exit is called directly).
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	configPath := os.Getenv("LOGO_CONFIG_PATH")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Set up structured logging with zap.
	var logger *zap.Logger
	if cfg.Log.Level == "debug" {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		return fmt.Errorf("creating logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	// Initialize storage: SQLite database + filesystem for logo PNGs.
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
		return fmt.Errorf("creating filesystem storage: %w", err)
	}

	logoRepo := storage.NewLogoRepository(db)
	llmCallRepo := storage.NewLLMCallRepository(db)
	processor := service.NewImageProcessor(fs)
	ghProvider := provider.NewGitHubProvider(cfg.GitHub.Repos, logger)

	// Build LLM clients in the configured order.
	// Only clients with API keys are created — missing keys mean that provider is skipped.
	llmProvider := buildLLMProvider(cfg, llmCallRepo, logger)

	// LogoService is the core orchestrator: cache → GitHub → LLM
	logoService := service.NewLogoService(logoRepo, fs, processor, ghProvider, llmProvider, logger)

	logger.Info("storage initialized",
		zap.String("database", cfg.Storage.DatabasePath),
		zap.String("logo_dir", cfg.Storage.LogoDir),
	)

	if llmProvider != nil {
		logger.Info("LLM providers configured",
			zap.Strings("provider_order", cfg.LLM.ProviderOrder),
		)
	} else {
		logger.Warn("no LLM providers configured — logo discovery limited to GitHub repos")
	}

	// Create and start the HTTP server
	deps := server.Deps{
		LogoRepo:       logoRepo,
		LLMCallRepo:    llmCallRepo,
		FileSystem:     fs,
		GitHubProvider: ghProvider,
		LLMProvider:    llmProvider,
		ImageProcessor: processor,
		LogoService:    logoService,
	}
	srv := server.New(cfg, logger, deps)

	// Graceful shutdown: listen for SIGINT (Ctrl+C) or SIGTERM (docker stop).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start()
	}()

	// Block until we receive a signal or the server errors out.
	select {
	case sig := <-quit:
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	case err := <-errChan:
		if err != nil {
			return err
		}
	}

	// Give in-flight requests 10 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.Shutdown(ctx)
}

// buildLLMProvider creates an LLM provider with clients in the configured order.
// Returns nil if no LLM API keys are configured — the service will skip the LLM layer.
//
// Go note: extracting this into a function keeps run() clean. In Go, you compose
// complex initialization from small, focused functions rather than using a DI framework.
func buildLLMProvider(cfg *config.Config, llmCallRepo storage.LLMCallRepository, logger *zap.Logger) *provider.LLMProvider {
	var clients []llm.Client

	for _, name := range cfg.LLM.ProviderOrder {
		switch name {
		case "anthropic":
			apiKey := cfg.LLM.Anthropic.APIKey
			if apiKey == "" {
				apiKey = os.Getenv("LOGO_LLM_ANTHROPIC_API_KEY")
			}
			if apiKey != "" {
				clients = append(clients, llm.NewAnthropicClient(apiKey, cfg.LLM.Anthropic.Model))
				logger.Info("LLM provider added", zap.String("provider", "anthropic"), zap.String("model", cfg.LLM.Anthropic.Model))
			}

		case "openai":
			apiKey := cfg.LLM.OpenAI.APIKey
			if apiKey == "" {
				apiKey = os.Getenv("LOGO_LLM_OPENAI_API_KEY")
			}
			if apiKey != "" {
				clients = append(clients, llm.NewOpenAIClient(apiKey, cfg.LLM.OpenAI.Model))
				logger.Info("LLM provider added", zap.String("provider", "openai"), zap.String("model", cfg.LLM.OpenAI.Model))
			}

		default:
			logger.Warn("unknown LLM provider in config, skipping", zap.String("provider", name))
		}
	}

	if len(clients) == 0 {
		return nil
	}

	return provider.NewLLMProvider(clients, cfg.LLM.RatePerMinute, llmCallRepo, logger)
}
