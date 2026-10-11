package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

const maxUpload = 2 << 20 // 2 MB

var uploadExt = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
	"image/x-icon": ".ico", "image/vnd.microsoft.icon": ".ico", "image/svg+xml": ".svg",
}

// sniffImage returns the image type of data, or "" if it is not an image we accept.
func sniffImage(data []byte) string {
	mime := http.DetectContentType(data)
	if _, ok := uploadExt[mime]; ok {
		return mime
	}
	head := bytes.ToLower(data[:min(len(data), 1024)])
	if bytes.Contains(head, []byte("<svg")) {
		return "image/svg+xml"
	}
	return ""
}

// handleUpload stores an image (multipart field "file") and returns its URL.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+64<<10)
	f, _, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "the image must be 2 MB or smaller")
			return
		}
		writeErr(w, http.StatusBadRequest, "send the image as the form field \"file\"")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUpload+1))
	if err != nil || len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "could not read the image")
		return
	}
	if len(data) > maxUpload {
		writeErr(w, http.StatusRequestEntityTooLarge, "the image must be 2 MB or smaller")
		return
	}
	mime := sniffImage(data)
	if mime == "" {
		writeErr(w, http.StatusBadRequest, "use a PNG, JPEG, WebP, GIF, SVG or ICO image")
		return
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	u := &store.Upload{Name: hex.EncodeToString(b) + uploadExt[mime], Mime: mime, Data: data}
	if err := s.db.SaveUpload(r.Context(), u); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"url": store.UploadPrefix + u.Name, "size": len(data), "mime": mime})
}

func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	u, err := s.db.GetUpload(r.Context(), r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", u.Mime)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	// An SVG opened on its own must not run anything.
	h.Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	if strings.HasSuffix(u.Name, ".svg") {
		h.Set("Content-Disposition", "inline")
	}
	http.ServeContent(w, r, u.Name, time.Unix(u.CreatedAt, 0), bytes.NewReader(u.Data))
}
