package middleware

import (
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-chat/internal/metrics"
)

// SecurityHeaders sets conservative browser security headers on every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' https://fonts.gstatic.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; "+
				"form-action 'self'; object-src 'none'")
		next.ServeHTTP(w, r)
	})
}

// SameOrigin rejects state-changing requests whose Origin header names
// another site. SameSite=Lax cookies already stop cross-site requests; this
// also stops other apps on the same host (a different port is "same site").
func SameOrigin(allowed []string, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Non-browser clients (curl, the eval runner) send no Origin;
				// browsers always do on cross-origin POSTs.
				next.ServeHTTP(w, r)
				return
			}
			u, err := url.Parse(origin)
			host := r.Host
			if fh := r.Header.Get("X-Forwarded-Host"); trustProxy && fh != "" {
				host = fh
			}
			if err != nil || (!strings.EqualFold(u.Host, host) && !slices.Contains(allowed, origin)) {
				http.Error(w, "Cross-origin request rejected", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter is a per-key token bucket: up to burst requests at once,
// refilled at rate per second. Idle keys are forgotten.
type RateLimiter struct {
	name  string
	rate  float64
	burst float64

	mu      sync.Mutex
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(name string, perMinute, burst int) *RateLimiter {
	return &RateLimiter{name: name, rate: float64(perMinute) / 60, burst: float64(burst), buckets: map[string]*bucket{}, lastGC: time.Now()}
}

// Allow reports whether key may make a request now; if not, it also
// returns how long until it may.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 30*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// KeyFunc picks the rate-limit key for a request.
type KeyFunc func(r *http.Request) string

// ByIP keys on the client address (X-Forwarded-For only when trusted).
func ByIP(trustProxy bool) KeyFunc {
	return func(r *http.Request) string {
		if trustProxy {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				first, _, _ := strings.Cut(xff, ",")
				return strings.TrimSpace(first)
			}
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
}

// ByUser keys on the authenticated user (use inside RequireAuth).
func ByUser(r *http.Request) string { return UserID(r) }

func (l *RateLimiter) Limit(key KeyFunc, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, wait := l.Allow(key(r))
		if !ok {
			metrics.RateLimited.WithLabelValues(l.name).Inc()
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, "Too many requests, please slow down", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
