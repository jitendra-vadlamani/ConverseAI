package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"ai-chat/internal/metrics"
)

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	userID      string
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// Logger logs one structured line per request, records latency metrics by
// route pattern, and turns panics into 500s.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("panic in handler", "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				if !rw.wroteHeader {
					http.Error(rw, "Internal server error", http.StatusInternalServerError)
				}
				rw.status = http.StatusInternalServerError
			}
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			elapsed := time.Since(start)
			metrics.HTTPRequestDuration.WithLabelValues(route, r.Method, strconv.Itoa(rw.status)).Observe(elapsed.Seconds())
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
				return
			}
			user := rw.userID
			if user == "" {
				user = "anonymous"
			}
			slog.Info("http request", "method", r.Method, "path", r.URL.Path, "status", rw.status,
				"duration_ms", elapsed.Milliseconds(), "user", user)
		}()
		next.ServeHTTP(rw, r)
	})
}
