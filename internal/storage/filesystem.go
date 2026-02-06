package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fleveque/logo-service/internal/model"
)

// FileSystem handles reading and writing logo image files on disk.
// Logos are stored at: {baseDir}/{SYMBOL}/{size}.png
type FileSystem struct {
	baseDir string
}

// NewFileSystem creates a new FileSystem storage, ensuring the base directory exists.
func NewFileSystem(baseDir string) (*FileSystem, error) {
	// MkdirAll creates the directory and all parents (like mkdir -p).
	// 0755 is the Unix permission mode: owner rwx, group rx, others rx.
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating logo directory: %w", err)
	}
	return &FileSystem{baseDir: baseDir}, nil
}

// LogoPath returns the filesystem path for a logo at a given size.
func (fs *FileSystem) LogoPath(symbol string, size model.LogoSize) string {
	return filepath.Join(fs.baseDir, symbol, string(size)+".png")
}

// SymbolDir returns the directory for a symbol's logos.
func (fs *FileSystem) SymbolDir(symbol string) string {
	return filepath.Join(fs.baseDir, symbol)
}

// Read reads a logo file from disk. Returns the raw PNG bytes.
// In Go, file I/O returns []byte (byte slice) — the fundamental type for binary data.
func (fs *FileSystem) Read(symbol string, size model.LogoSize) ([]byte, error) {
	path := fs.LogoPath(symbol, size)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("logo file not found: %s/%s", symbol, size)
		}
		return nil, fmt.Errorf("reading logo file: %w", err)
	}
	return data, nil
}

// Write saves a logo PNG to disk, creating the symbol directory if needed.
func (fs *FileSystem) Write(symbol string, size model.LogoSize, data []byte) error {
	dir := fs.SymbolDir(symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating symbol directory: %w", err)
	}

	path := fs.LogoPath(symbol, size)
	// WriteFile atomically writes the file (creates or truncates).
	// 0644: owner rw, group r, others r — standard for non-executable files.
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing logo file: %w", err)
	}
	return nil
}

// Exists checks if a logo file exists on disk.
func (fs *FileSystem) Exists(symbol string, size model.LogoSize) bool {
	path := fs.LogoPath(symbol, size)
	_, err := os.Stat(path)
	return err == nil
}

// DeleteSymbol removes all logo files for a symbol.
func (fs *FileSystem) DeleteSymbol(symbol string) error {
	dir := fs.SymbolDir(symbol)
	return os.RemoveAll(dir)
}
