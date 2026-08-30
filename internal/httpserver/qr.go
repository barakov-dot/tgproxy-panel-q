package httpserver

import (
	"net/http"

	"github.com/barakov-dot/tgproxy-panel/internal/qrcode"
)

// qrSize is the pixel width/height of the generated QR PNG — large enough
// to scan comfortably off a phone screen showing the panel.
const qrSize = 300

func (s *Server) handleUserQR(w http.ResponseWriter, r *http.Request) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	if u.Secret == nil {
		http.NotFound(w, r)
		return
	}

	link := profileLink(s.cfg.TproxyHostname, *u.Secret)
	png, err := qrcode.PNG(link, qrSize)
	if err != nil {
		s.log.Error("generate qr", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	// Never cache-friendly across users: a revoke+reissue reuses the same
	// user detail URL but the secret (and thus the QR) changes.
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}
