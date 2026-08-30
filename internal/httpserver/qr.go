package httpserver

import (
	"net/http"

	"github.com/barakov-dot/tgproxy-panel-q/internal/qrcode"
)

const qrSize = 300

func (s *Server) handleUserQR(w http.ResponseWriter, r *http.Request) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}

	link := s.svc.GetProxyLink(u)
	if link == "" {
		http.NotFound(w, r)
		return
	}

	png, err := qrcode.GeneratePNG(link, qrSize)
	if err != nil {
		s.log.Error("generate qr", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}
