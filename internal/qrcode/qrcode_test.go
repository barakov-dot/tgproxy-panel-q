package qrcode

import (
	"bytes"
	"testing"
)

var pngMagic = []byte{0x89, 'P', 'N', 'G'}

func TestGeneratePNG(t *testing.T) {
	cases := []struct {
		name string
		link string
		size int
	}{
		{
			name: "proxy deep link",
			link: "https://t.me/webproxy?server=proxy.example.com&secret=abc123",
			size: 256,
		},
		{
			name: "short link",
			link: "https://example.com",
			size: 128,
		},
		{
			name: "minimal size",
			link: "hello",
			size: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			png, err := GeneratePNG(tc.link, tc.size)
			if err != nil {
				t.Fatalf("GeneratePNG() error = %v", err)
			}
			if len(png) == 0 {
				t.Fatal("GeneratePNG() returned empty output")
			}
			if !bytes.HasPrefix(png, pngMagic) {
				t.Fatalf("GeneratePNG() output does not start with PNG magic bytes, got %v", png[:4])
			}
		})
	}
}

func TestGeneratePNGValidation(t *testing.T) {
	cases := []struct {
		name string
		link string
		size int
	}{
		{name: "empty link", link: "", size: 256},
		{name: "zero size", link: "hello", size: 0},
		{name: "negative size", link: "hello", size: -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := GeneratePNG(tc.link, tc.size); err == nil {
				t.Fatal("GeneratePNG() expected error")
			}
		})
	}
}
