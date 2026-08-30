// Package qrcode is a thin wrapper around skip2/go-qrcode, sized for the two
// places tgproxy-panel needs a QR code: the admin panel's user detail page
// and the bot's "here's your proxy link" message (CLAUDE.md's
// directory-structure bullet for this package).
package qrcode

import (
	"fmt"

	goqrcode "github.com/skip2/go-qrcode"
)

// PNG renders content (typically a t.me deep link) as a PNG-encoded QR code
// image, size pixels square. Error-correction is left at the library's
// default (medium), which is more than enough for a short URL.
func PNG(content string, size int) ([]byte, error) {
	if content == "" {
		return nil, fmt.Errorf("qrcode: content must not be empty")
	}
	if size <= 0 {
		return nil, fmt.Errorf("qrcode: size must be positive, got %d", size)
	}
	png, err := goqrcode.Encode(content, goqrcode.Medium, size)
	if err != nil {
		return nil, fmt.Errorf("qrcode: encode: %w", err)
	}
	return png, nil
}
