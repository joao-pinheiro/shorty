package qr

import (
	"bytes"
	"testing"
)

var pngHeader = []byte{0x89, 0x50, 0x4e, 0x47}

func TestGenerate_ValidPNG(t *testing.T) {
	data, err := Generate("https://example.com", 256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.HasPrefix(data, pngHeader) {
		t.Error("output is not a valid PNG")
	}
}

func TestGenerate_SizeClampedLow(t *testing.T) {
	data, err := Generate("https://example.com", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.HasPrefix(data, pngHeader) {
		t.Error("output is not a valid PNG")
	}
}

func TestGenerate_SizeClampedHigh(t *testing.T) {
	data, err := Generate("https://example.com", 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.HasPrefix(data, pngHeader) {
		t.Error("output is not a valid PNG")
	}
}

func TestGenerate_DifferentURLsProduceDifferentPNGs(t *testing.T) {
	a, err := Generate("https://a.com", 256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := Generate("https://b.com", 256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Error("different URLs should produce different PNGs")
	}
}

func TestGenerate_EmptyURL(t *testing.T) {
	_, err := Generate("", 256)
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}
