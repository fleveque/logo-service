package service

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/h2non/bimg"

	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/storage"
)

// createTestPNG generates a small solid-color PNG image in memory.
// Go's standard library includes image encoding/decoding — no external deps needed.
// image.NRGBA is a non-premultiplied alpha image (common for PNGs with transparency).
func createTestPNG(width, height int, c color.Color) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // only in tests — panics are acceptable for impossible failures
	}
	return buf.Bytes()
}

func TestProcessAll(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("creating filesystem: %v", err)
	}

	processor := NewImageProcessor(fs)

	// Create a 256x256 red test image
	testImage := createTestPNG(256, 256, color.RGBA{R: 255, G: 0, B: 0, A: 255})

	results, err := processor.ProcessAll("TEST", testImage)
	if err != nil {
		t.Fatalf("ProcessAll failed: %v", err)
	}

	// All sizes should succeed
	for _, size := range model.AllSizes {
		if !results[size] {
			t.Errorf("expected size %s to succeed", size)
		}

		// Verify the file exists on disk
		if !fs.Exists("TEST", size) {
			t.Errorf("expected file to exist for size %s", size)
			continue
		}

		// Read it back and verify dimensions
		data, err := fs.Read("TEST", size)
		if err != nil {
			t.Errorf("reading size %s: %v", size, err)
			continue
		}

		imgSize, err := bimg.NewImage(data).Size()
		if err != nil {
			t.Errorf("getting size for %s: %v", size, err)
			continue
		}

		expectedPx := model.SizePixels[size]
		if imgSize.Width != expectedPx || imgSize.Height != expectedPx {
			t.Errorf("size %s: expected %dx%d, got %dx%d",
				size, expectedPx, expectedPx, imgSize.Width, imgSize.Height)
		}
	}
}

func TestProcessAll_NonSquareImage(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("creating filesystem: %v", err)
	}

	processor := NewImageProcessor(fs)

	// Create a rectangular image (wider than tall)
	testImage := createTestPNG(400, 200, color.RGBA{R: 0, G: 0, B: 255, A: 255})

	results, err := processor.ProcessAll("RECT", testImage)
	if err != nil {
		t.Fatalf("ProcessAll failed: %v", err)
	}

	// All sizes should still succeed (bimg embeds into a square canvas)
	for _, size := range model.AllSizes {
		if !results[size] {
			t.Errorf("expected size %s to succeed for non-square image", size)
		}
	}
}

func TestApplyBackground(t *testing.T) {
	// Create a semi-transparent test image
	testImage := createTestPNG(64, 64, color.NRGBA{R: 255, G: 0, B: 0, A: 128})

	result, err := ApplyBackground(testImage, "ffffff")
	if err != nil {
		t.Fatalf("ApplyBackground failed: %v", err)
	}

	// Result should be valid PNG data
	if len(result) == 0 {
		t.Error("expected non-empty result")
	}

	// Verify it's a valid image
	size, err := bimg.NewImage(result).Size()
	if err != nil {
		t.Fatalf("getting result size: %v", err)
	}
	if size.Width != 64 || size.Height != 64 {
		t.Errorf("expected 64x64, got %dx%d", size.Width, size.Height)
	}
}

func TestApplyBackground_WithHash(t *testing.T) {
	testImage := createTestPNG(32, 32, color.RGBA{R: 0, G: 255, B: 0, A: 255})

	// Should work with # prefix too
	_, err := ApplyBackground(testImage, "#ff0000")
	if err != nil {
		t.Fatalf("ApplyBackground with # prefix failed: %v", err)
	}
}

func TestParseHexColor(t *testing.T) {
	// Go table-driven tests: define test cases as a slice of structs,
	// then loop over them. This is the idiomatic way to test multiple inputs.
	tests := []struct {
		name    string
		hex     string
		wantR   uint8
		wantG   uint8
		wantB   uint8
		wantErr bool
	}{
		{"white", "ffffff", 255, 255, 255, false},
		{"black", "000000", 0, 0, 0, false},
		{"red", "ff0000", 255, 0, 0, false},
		{"with hash", "#00ff00", 0, 255, 0, false},
		{"mixed case", "aaBBcc", 170, 187, 204, false},
		{"too short", "fff", 0, 0, 0, true},
		{"too long", "fffffff", 0, 0, 0, true},
		{"invalid chars", "gggggg", 0, 0, 0, true},
	}

	for _, tt := range tests {
		// t.Run creates a subtest — each case gets its own name in the output.
		// This is the Go equivalent of RSpec's `it "does something" do` blocks.
		t.Run(tt.name, func(t *testing.T) {
			r, g, b, err := parseHexColor(tt.hex)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseHexColor(%q) error = %v, wantErr = %v", tt.hex, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if r != tt.wantR || g != tt.wantG || b != tt.wantB {
					t.Errorf("parseHexColor(%q) = (%d,%d,%d), want (%d,%d,%d)",
						tt.hex, r, g, b, tt.wantR, tt.wantG, tt.wantB)
				}
			}
		})
	}
}
