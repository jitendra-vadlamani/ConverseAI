package handler

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Check is one readiness dependency.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
	// Optional checks are reported but don't fail readiness (e.g. Ollama
	// being down shouldn't take the UI offline).
	Optional bool
}

// Healthz reports that the process is up.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz checks every dependency in parallel.
func Readyz(checks []Check) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		results := make(map[string]string, len(checks))
		var mu sync.Mutex
		var wg sync.WaitGroup
		ready := true
		for _, c := range checks {
			wg.Add(1)
			go func(c Check) {
				defer wg.Done()
				status := "ok"
				if err := c.Fn(ctx); err != nil {
					status = "unavailable"
				}
				mu.Lock()
				results[c.Name] = status
				if status != "ok" && !c.Optional {
					ready = false
				}
				mu.Unlock()
			}(c)
		}
		wg.Wait()
		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{"ready": ready, "checks": results})
	}
}
