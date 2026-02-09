// Package provider defines the interface for logo acquisition sources.
// Each provider (GitHub, LLM) implements this interface to supply logos.
package provider

import "context"

// LogoResult represents a successfully acquired logo from a provider.
type LogoResult struct {
	Symbol      string
	CompanyName string
	ImageData   []byte // Raw image bytes (PNG/SVG/JPG/WebP)
	Source      string // e.g., "github:davidepalazzo/ticker-logos"
	OriginalURL string // Where the image was downloaded from
}

// ImportStats tracks the results of a bulk import operation.
type ImportStats struct {
	Total     int
	Imported  int
	Skipped   int // Already existed
	Failed    int
	Errors    []string
}

// LogoProvider is the interface for logo acquisition sources.
// Each implementation knows how to find and download logos for stock symbols.
type LogoProvider interface {
	// GetLogo fetches a single logo by symbol.
	GetLogo(ctx context.Context, symbol string) (*LogoResult, error)

	// BulkImport downloads all available logos from the source.
	// The callback is called for each logo found — this lets the caller
	// process logos one at a time without holding everything in memory.
	BulkImport(ctx context.Context, callback func(result *LogoResult) error) (*ImportStats, error)

	// Name returns a human-readable name for the provider.
	Name() string
}
