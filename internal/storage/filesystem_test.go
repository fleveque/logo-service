package storage

import (
	"testing"

	"github.com/fleveque/logo-service/internal/model"
)

func TestFileSystem_WriteAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("creating filesystem: %v", err)
	}

	// Write a fake PNG (just some bytes for testing)
	fakeImage := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	if err := fs.Write("AAPL", model.SizeM, fakeImage); err != nil {
		t.Fatalf("writing logo: %v", err)
	}

	// Verify it exists
	if !fs.Exists("AAPL", model.SizeM) {
		t.Error("expected logo to exist after write")
	}

	// Read it back
	data, err := fs.Read("AAPL", model.SizeM)
	if err != nil {
		t.Fatalf("reading logo: %v", err)
	}

	// Compare byte slices — Go doesn't have == for slices, use length + index comparison
	if len(data) != len(fakeImage) {
		t.Fatalf("expected %d bytes, got %d", len(fakeImage), len(data))
	}
	for i, b := range data {
		if b != fakeImage[i] {
			t.Errorf("byte %d: expected %x, got %x", i, fakeImage[i], b)
		}
	}
}

func TestFileSystem_Exists_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("creating filesystem: %v", err)
	}

	if fs.Exists("NOPE", model.SizeM) {
		t.Error("expected non-existent logo to return false")
	}
}

func TestFileSystem_Read_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("creating filesystem: %v", err)
	}

	_, err = fs.Read("NOPE", model.SizeM)
	if err == nil {
		t.Error("expected error reading non-existent logo")
	}
}

func TestFileSystem_DeleteSymbol(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("creating filesystem: %v", err)
	}

	// Write logos at multiple sizes
	data := []byte("test")
	for _, size := range model.AllSizes {
		if err := fs.Write("TSLA", size, data); err != nil {
			t.Fatalf("writing size %s: %v", size, err)
		}
	}

	// Delete the symbol
	if err := fs.DeleteSymbol("TSLA"); err != nil {
		t.Fatalf("deleting symbol: %v", err)
	}

	// Verify all sizes are gone
	for _, size := range model.AllSizes {
		if fs.Exists("TSLA", size) {
			t.Errorf("expected size %s to be deleted", size)
		}
	}
}

func TestFileSystem_LogoPath(t *testing.T) {
	fs := &FileSystem{baseDir: "/data/logos"}
	path := fs.LogoPath("AAPL", model.SizeM)
	expected := "/data/logos/AAPL/m.png"
	if path != expected {
		t.Errorf("expected path %s, got %s", expected, path)
	}
}
