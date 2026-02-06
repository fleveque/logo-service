// Package main is the entry point for the logo-service HTTP server.
// In Go, the `main` package with a `main()` function is what gets executed.
// Unlike Ruby/JS, Go compiles to a single static binary — no runtime needed.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/config"
	"github.com/fleveque/logo-service/internal/server"
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
	// zap is a high-performance structured logger — it outputs JSON in production
	// and human-readable format in development.
	var logger *zap.Logger
	if cfg.Log.Level == "debug" {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		return fmt.Errorf("creating logger: %w", err)
	}
	// defer runs when the enclosing function returns — like Ruby's ensure or
	// a finally block. Great for cleanup.
	// Sync flushes buffered log entries. We intentionally ignore the error here
	// because Sync commonly fails on stdout/stderr (not a real problem).
	defer func() { _ = logger.Sync() }()

	// Create and start the HTTP server
	srv := server.New(cfg, logger)

	// Graceful shutdown: listen for SIGINT (Ctrl+C) or SIGTERM (docker stop).
	// Channels are Go's primary concurrency primitive — goroutines communicate
	// through channels instead of sharing memory.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine (lightweight thread managed by Go runtime).
	// The `go` keyword spawns a goroutine — it's like starting a background task.
	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start()
	}()

	// Block until we receive a signal or the server errors out.
	// select is like a switch for channels — it waits until one is ready.
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
