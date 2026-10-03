package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"ai-chat/internal/service"
)

const maxJSONBody = 1 << 20

var errUnsupportedMedia = errors.New("Content-Type must be application/json")

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps service errors to status codes. Only messages meant for
// users are returned; anything unexpected is logged and reported as a
// generic 500 so internals never leak to clients.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *service.ValidationError
	var mbe *http.MaxBytesError
	status, msg := http.StatusInternalServerError, "Internal server error"
	switch {
	case errors.As(err, &ve):
		status, msg = http.StatusBadRequest, ve.Msg
	case errors.As(err, &mbe):
		status, msg = http.StatusRequestEntityTooLarge, fmt.Sprintf("Request is too large (max %d MB)", mbe.Limit>>20)
	case errors.Is(err, errUnsupportedMedia):
		status, msg = http.StatusUnsupportedMediaType, err.Error()
	case errors.Is(err, errBadJSON):
		status, msg = http.StatusBadRequest, "Invalid request body"
	case errors.Is(err, service.ErrInvalidCredentials):
		status, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, service.ErrEmailTaken):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrNotFound):
		status, msg = http.StatusNotFound, "Not found"
	case errors.Is(err, service.ErrConflict):
		status, msg = http.StatusConflict, err.Error()
	default:
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	http.Error(w, msg, status)
}

var errBadJSON = errors.New("invalid JSON body")

// decodeJSON requires a JSON content type (which also blocks simple
// cross-site form posts) and bounds the body size.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		return errUnsupportedMedia
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return err
		}
		return errBadJSON
	}
	return nil
}

// sse writes Server-Sent Events. Each payload is JSON on a single data
// line, so text containing newlines can't break the framing.
type sse struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newSSE(w http.ResponseWriter) *sse {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	// Streams outlive the server's write timeout.
	_ = rc.SetWriteDeadline(time.Time{})
	return &sse{w: w, rc: rc}
}

func (s *sse) event(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	return s.rc.Flush()
}

func (s *sse) comment(text string) error {
	if _, err := io.WriteString(s.w, ": "+text+"\n\n"); err != nil {
		return err
	}
	return s.rc.Flush()
}
