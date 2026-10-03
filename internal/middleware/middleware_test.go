package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiterBurstThenRefuse(t *testing.T) {
	l := NewRateLimiter("t", 60, 3)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("request %d should pass", i)
		}
	}
	ok, wait := l.Allow("k")
	if ok || wait <= 0 {
		t.Fatal("4th request should be limited with a wait")
	}
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("keys are independent")
	}
	rec := httptest.NewRecorder()
	l.Limit(func(*http.Request) string { return "k" }, func(w http.ResponseWriter, r *http.Request) {})(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d %v", rec.Code, rec.Header())
	}
}

func TestSameOrigin(t *testing.T) {
	h := SameOrigin([]string{"http://localhost:5173"}, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cases := []struct {
		method, origin string
		want           int
	}{
		{"POST", "", 200},
		{"POST", "http://app.example:8080", 200},
		{"POST", "http://localhost:5173", 200},
		{"POST", "http://evil.example", 403},
		{"DELETE", "http://app.example:9999", 403}, // other app on the same host
		{"GET", "http://evil.example", 200},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, "http://app.example:8080/api/x", nil)
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s from %q: got %d want %d", c.method, c.origin, rec.Code, c.want)
		}
	}
}

func TestByIPIgnoresForwardedForUnlessTrusted(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
	if ByIP(false)(req) != "10.0.0.5" || ByIP(true)(req) != "1.2.3.4" {
		t.Fatal("wrong client key")
	}
}

func TestLoggerRecoversPanics(t *testing.T) {
	h := Logger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 500 {
		t.Fatalf("got %d", rec.Code)
	}
}
