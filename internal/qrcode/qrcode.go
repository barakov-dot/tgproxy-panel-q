// Package qrcode is a thin wrapper around skip2/go-qrcode for proxy link QR images.
package qrcode

import (
	"fmt"

	goqrcode "github.com/skip2/go-qrcode"
)

// GeneratePNG renders link as a PNG-encoded QR code image, size pixels square.
func GeneratePNG(link string, size int) ([]byte, error) {
	if link == "" {
		return nil, fmt.Errorf("qrcode: link must not be empty")
	}
	if size <= 0 {
		return nil, fmt.Errorf("qrcode: size must be positive, got %d", size)
	}
	png, err := goqrcode.Encode(link, goqrcode.Medium, size)
	if err != nil {
		return nil, fmt.Errorf("qrcode: encode: %w", err)
	}
	return png, nil
}
