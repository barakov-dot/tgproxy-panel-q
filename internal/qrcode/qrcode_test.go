package qrcode

import "testing"

func TestPNG(t *testing.T) {
	png, err := PNG("https://t.me/webproxy?server=proxy.example.com&secret=abc123", 256)
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	if len(png) == 0 {
		t.Fatal("PNG: got empty output")
	}
	// PNG magic number.
	want := []byte{0x89, 'P', 'N', 'G'}
	for i, b := range want {
		if png[i] != b {
			t.Fatalf("PNG: output does not start with PNG magic bytes, got %v", png[:4])
		}
	}
}

func TestPNGValidation(t *testing.T) {
	if _, err := PNG("", 256); err == nil {
		t.Error("PNG: expected error for empty content")
	}
	if _, err := PNG("hello", 0); err == nil {
		t.Error("PNG: expected error for zero size")
	}
	if _, err := PNG("hello", -1); err == nil {
		t.Error("PNG: expected error for negative size")
	}
}
