// Package service contains the core business logic for the logo pipeline.
package service

import (
	"fmt"
	"strings"

	"github.com/h2non/bimg"

	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/storage"
)

// ImageProcessor handles resizing and background color application for logos.
// It uses bimg (Go bindings for libvips) — a C library that's extremely fast
// at image manipulation. The trade-off: requires libvips as a system dependency.
type ImageProcessor struct {
	fs *storage.FileSystem
}

// NewImageProcessor creates a new ImageProcessor.
func NewImageProcessor(fs *storage.FileSystem) *ImageProcessor {
	return &ImageProcessor{fs: fs}
}

// ProcessAll takes raw image bytes (any format bimg supports: PNG, JPEG, SVG, WebP)
// and creates resized PNGs for all sizes, saving them to the filesystem.
//
// Go note: returning a map lets the caller know which sizes succeeded.
// We process all sizes even if some fail, collecting errors along the way.
func (p *ImageProcessor) ProcessAll(symbol string, imageData []byte) (map[model.LogoSize]bool, error) {
	results := make(map[model.LogoSize]bool)
	var errs []string

	for _, size := range model.AllSizes {
		pixels := model.SizePixels[size]
		resized, err := resizeToSquarePNG(imageData, pixels)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", size, err))
			results[size] = false
			continue
		}

		if err := p.fs.Write(symbol, size, resized); err != nil {
			errs = append(errs, fmt.Sprintf("%s write: %v", size, err))
			results[size] = false
			continue
		}

		results[size] = true
	}

	if len(errs) > 0 {
		return results, fmt.Errorf("processing errors: %s", strings.Join(errs, "; "))
	}

	return results, nil
}

// resizeToSquarePNG resizes an image to a square PNG of the given pixel size.
// bimg.Options is a struct with many fields — this is Go's alternative to
// builder patterns or method chaining. You set only the fields you need.
func resizeToSquarePNG(imageData []byte, pixels int) ([]byte, error) {
	// bimg.NewImage wraps raw bytes — it doesn't copy them, just references them.
	img := bimg.NewImage(imageData)

	// First, resize to a square. bimg handles aspect ratio and format detection.
	resized, err := img.Process(bimg.Options{
		Width:   pixels,
		Height:  pixels,
		Type:    bimg.PNG,
		Embed:   true,              // Embed in a canvas if aspect ratio doesn't match
		Enlarge: true,              // Allow upscaling if source is smaller
		Background: bimg.Color{     // Transparent background for the canvas
			R: 0, G: 0, B: 0,
		},
		Interpretation: bimg.InterpretationSRGB,
	})
	if err != nil {
		return nil, fmt.Errorf("resizing to %dpx: %w", pixels, err)
	}

	return resized, nil
}

// ApplyBackground takes a PNG and flattens the alpha channel onto a solid
// background color. This is used at request time when the `bg` query param
// is provided — the cached transparent PNG gets a background on the fly.
//
// Go note: hex color parsing is done manually here. In Go, you often write
// small utility functions instead of pulling in a library for simple tasks.
func ApplyBackground(imageData []byte, hexColor string) ([]byte, error) {
	r, g, b, err := parseHexColor(hexColor)
	if err != nil {
		return nil, err
	}

	img := bimg.NewImage(imageData)
	return img.Process(bimg.Options{
		Background: bimg.Color{R: r, G: g, B: b},
		Type:       bimg.PNG,
		Interpretation: bimg.InterpretationSRGB,
	})
}

// parseHexColor converts a hex color string (with or without #) to RGB values.
// Go's fmt.Sscanf is like C's scanf — it parses formatted strings.
func parseHexColor(hex string) (uint8, uint8, uint8, error) {
	hex = strings.TrimPrefix(hex, "#")

	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid hex color: %q (expected 6 characters)", hex)
	}

	var r, g, b uint8
	_, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parsing hex color %q: %w", hex, err)
	}

	return r, g, b, nil
}
