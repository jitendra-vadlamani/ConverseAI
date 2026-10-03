package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestIsPublicAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.29.178", "169.254.169.254",
		"100.108.82.77", "0.0.0.0", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "224.0.0.1",
	}
	for _, s := range blocked {
		if IsPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !IsPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestSafeClientRefusesInternalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("internal secret"))
	}))
	defer srv.Close()

	s := &service{client: NewSafeClient(5 * time.Second)}
	_, err := s.FetchPageContent(context.Background(), srv.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("fetching a loopback URL must be blocked, got %v", err)
	}
	for _, u := range []string{"file:///etc/passwd", "gopher://x", "http://user:pw@example.com/"} {
		if _, err := s.FetchPageContent(context.Background(), u); err == nil {
			t.Errorf("%s should be rejected", u)
		}
	}
}

func TestParseDuckDuckGo(t *testing.T) {
	page := `
<div class="result results_links results_links_deep web-result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2Fgo1.24&amp;rut=abc">Go 1.24 <b>Release</b> Notes</a>
  <a class="result__snippet" href="x">Released Feb 11, 2025. Generic type aliases &amp; more.</a>
</div>
<div class="result">
  <a rel="nofollow" class="result__a" href="https://duckduckgo.com/y.js?ad_provider=x">Ad</a>
  <a class="result__snippet" href="x">sponsored</a>
</div>`
	got := parseDuckDuckGo(page)
	if len(got) != 1 {
		t.Fatalf("want 1 result (ad skipped), got %d: %+v", len(got), got)
	}
	r := got[0]
	if r.URL != "https://go.dev/doc/go1.24" || r.Source != "go.dev" || r.AuthorityScore != 1 {
		t.Errorf("bad result: %+v", r)
	}
	if r.Content != "Go 1.24 Release Notes: Released Feb 11, 2025. Generic type aliases & more." {
		t.Errorf("content: %q", r.Content)
	}
}

func TestFreshness(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	cases := map[string]float64{
		"no date here":             0.7,
		"updated 2026-10-03":       1.0,
		"Published Oct 3, 2021":    0.1,
		"Published January 2 2026": 1 - (now.Sub(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)).Hours()/24/365)/5,
	}
	for text, want := range cases {
		if got := Freshness(text, now); got < want-0.01 || got > want+0.01 {
			t.Errorf("Freshness(%q) = %.3f, want %.3f", text, got, want)
		}
	}
}

func TestAuthority(t *testing.T) {
	if Authority("www.github.com") != 1 || Authority("docs.github.com") != 1 || Authority("nasa.gov") != 0.9 || Authority("random.blog") != 0.4 {
		t.Error("authority tiers wrong")
	}
}
