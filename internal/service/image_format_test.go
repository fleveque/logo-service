package service

import (
	"strings"
	"testing"
)

func TestValidateImageFormat_AcceptsKnownRasterTypes(t *testing.T) {
	cases := map[string][]byte{
		"PNG":  {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x00, 0x00, 0x0d},
		"JPEG": {0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01},
		"WebP": []byte("RIFF\x00\x00\x00\x00WEBPVP8L\x00\x00\x00\x00"),
		"GIF":  []byte("GIF89a\x10\x00\x10\x00\xff\xff"),
	}
	for name, data := range cases {
		if err := validateImageFormat(data); err != nil {
			t.Errorf("%s rejected unexpectedly: %v", name, err)
		}
	}
}

func TestValidateImageFormat_RejectsSVG(t *testing.T) {
	cases := [][]byte{
		[]byte(`<?xml version="1.0" encoding="UTF-8"?><svg ...>`),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"></svg>`),
		[]byte("  \n<?xml version=\"1.0\"?>\n<svg></svg>"),
	}
	for i, data := range cases {
		err := validateImageFormat(data)
		if err == nil {
			t.Errorf("case %d: SVG should be rejected", i)
			continue
		}
		if !strings.Contains(err.Error(), "SVG") {
			t.Errorf("case %d: error should mention SVG, got %v", i, err)
		}
	}
}

func TestValidateImageFormat_RejectsHTML(t *testing.T) {
	cases := [][]byte{
		[]byte("<!DOCTYPE html><html><body>not an image</body></html>"),
		[]byte("<html><head><title>404</title></head></html>"),
	}
	for i, data := range cases {
		err := validateImageFormat(data)
		if err == nil {
			t.Errorf("case %d: HTML should be rejected", i)
			continue
		}
		if !strings.Contains(err.Error(), "HTML") {
			t.Errorf("case %d: error should mention HTML, got %v", i, err)
		}
	}
}

func TestValidateImageFormat_RejectsShortInput(t *testing.T) {
	if err := validateImageFormat([]byte{0x89, 'P'}); err == nil {
		t.Fatal("expected error on tiny input")
	}
}

func TestValidateImageFormat_RejectsUnknownBytes(t *testing.T) {
	data := []byte("Hello world, definitely not an image at all")
	if err := validateImageFormat(data); err == nil {
		t.Fatal("expected error on garbage input")
	}
}
