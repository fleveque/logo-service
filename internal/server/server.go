package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fleveque/logo-service/internal/config"
	"github.com/fleveque/logo-service/internal/storage"
)

// Deps groups all dependencies the server needs. Using a struct instead of
// many function parameters keeps the constructor clean as dependencies grow.
// This is called the "functional options" alternative — a simple deps struct.
type Deps struct {
	LogoRepo    storage.LogoRepository
	LLMCallRepo storage.LLMCallRepository
	FileSystem  *storage.FileSystem
}

// Server wraps the HTTP server and its dependencies.
// In Go, you typically compose a struct with all the pieces your server needs,
// then wire them together in the constructor (New function).
type Server struct {
	cfg    *config.Config
	deps   Deps
	router *gin.Engine
	logger *zap.Logger
	http   *http.Server
}

// New creates and configures a new Server.
func New(cfg *config.Config, logger *zap.Logger, logoRepo storage.LogoRepository, llmCallRepo storage.LLMCallRepository, fs *storage.FileSystem) *Server {
	// Set Gin mode based on log level
	if cfg.Log.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Recovery middleware catches panics and returns 500 instead of crashing.
	router.Use(gin.Recovery())

	// Register routes
	RegisterRoutes(router)

	s := &Server{
		cfg: cfg,
		deps: Deps{
			LogoRepo:    logoRepo,
			LLMCallRepo: llmCallRepo,
			FileSystem:  fs,
		},
		router: router,
		logger: logger,
		http: &http.Server{
			Addr:         cfg.Server.Address(),
			Handler:      router,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	return s
}

// Start begins listening for HTTP requests. This blocks until the server stops.
func (s *Server) Start() error {
	s.logger.Info("starting server", zap.String("address", s.cfg.Server.Address()))
	if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server listen: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server, waiting for in-flight requests to complete.
// context.Context is Go's way of handling cancellation and timeouts — you'll see it everywhere.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down server")
	return s.http.Shutdown(ctx)
}

// Router returns the underlying Gin engine (useful for testing).
func (s *Server) Router() *gin.Engine {
	return s.router
}
