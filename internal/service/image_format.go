package service

import (
	"bytes"
	"fmt"
	"strings"
)

// validateImageFormat returns nil if data starts with magic bytes for a raster
// image format our libvips build can process. Otherwise it returns a specific
// error so the caller can log why the bytes were rejected (instead of waiting
// for libvips to time out or emit its generic "Unsupported image format").
//
// We explicitly reject SVG: the Alpine `vips` package this image runs on was
// compiled without rsvg support, so SVG bytes would hang the resize pipeline.
// The Wikidata provider already requests pre-rasterized PNGs via Wikimedia's
// `?width=` endpoint; this guard catches anything else that slips through
// (e.g. an LLM returning a Wikipedia file *page* URL whose body is HTML).
func validateImageFormat(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("data too short (%d bytes) to be an image", len(data))
	}

	// PNG: \x89 P N G \r \n \x1a \n
	if bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return nil
	}
	// JPEG: FF D8 FF
	if bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) {
		return nil
	}
	// WebP: "RIFF" .... "WEBP"
	if bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return nil
	}
	// GIF
	if bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return nil
	}

	// Look at the first chunk as text to give better errors on common non-image responses.
	headLen := 512
	if len(data) < headLen {
		headLen = len(data)
	}
	head := strings.ToLower(strings.TrimSpace(string(data[:headLen])))

	if strings.HasPrefix(head, "<?xml") || strings.HasPrefix(head, "<svg") {
		return fmt.Errorf("SVG input not supported (libvips compiled without rsvg)")
	}
	if strings.HasPrefix(head, "<!doctype") || strings.HasPrefix(head, "<html") {
		return fmt.Errorf("response is HTML, not an image")
	}

	return fmt.Errorf("unrecognized image format (first 16 bytes: %x)", data[:16])
}
