// Package search queries public search engines and fetches result pages.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/util"
)

const (
	maxPageBytes   = 2 << 20 // bytes read from any page
	maxPageText    = 12000   // bytes of text kept per page
	userAgent      = "Mozilla/5.0 (compatible; ConverseAI/1.0; +https://github.com/jitendra-vadlamani/ConverseAI)"
	maxResultCount = 6
)

var (
	dateRegex = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2}|(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\.?\s+\d{1,2},?\s+\d{4})\b`)

	ddgResult  = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippet = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</a>`)

	reScript  = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head)[^>]*>.*?</(script|style|noscript|svg|head)>`)
	reTags    = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces  = regexp.MustCompile(`\s+`)
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)

	authorityTiers = map[string]float64{
		"github.com":            1.0,
		"learn.microsoft.com":   1.0,
		"docs.microsoft.com":    1.0,
		"developer.mozilla.org": 1.0,
		"en.wikipedia.org":      1.0,
		"aws.amazon.com":        1.0,
		"go.dev":                1.0,
		"python.org":            1.0,
		"docs.python.org":       1.0,
		"stackoverflow.com":     0.9,
		"stackexchange.com":     0.9,
		"dev.to":                0.8,
		"reddit.com":            0.6,
		"medium.com":            0.6,
	}
)

type Service interface {
	Search(ctx context.Context, query string) ([]model.Evidence, error)
	FetchPageContent(ctx context.Context, rawURL string) (string, error)
}

type service struct {
	client *http.Client
}

func NewService() Service {
	return &service{client: NewSafeClient(20 * time.Second)}
}

// Search queries DuckDuckGo and falls back to Wikipedia when DDG fails or
// returns nothing (DDG rate-limits scrapers aggressively).
func (s *service) Search(ctx context.Context, query string) ([]model.Evidence, error) {
	results, ddgErr := s.searchDuckDuckGo(ctx, query)
	if ddgErr == nil && len(results) > 0 {
		return results, nil
	}
	wiki, err := s.searchWikipedia(ctx, query)
	if err != nil {
		if ddgErr != nil {
			return nil, fmt.Errorf("duckduckgo: %v; wikipedia: %w", ddgErr, err)
		}
		return nil, err
	}
	return wiki, nil
}

func (s *service) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,application/json;q=0.8")
	return s.client.Do(req)
}

func (s *service) searchDuckDuckGo(ctx context.Context, query string) ([]model.Evidence, error) {
	resp, err := s.get(ctx, "https://html.duckduckgo.com/html/?q="+url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, err
	}
	return parseDuckDuckGo(string(body)), nil
}

// parseDuckDuckGo extracts results from html.duckduckgo.com markup, where
// links look like //duckduckgo.com/l/?uddg=<escaped target>&rut=...
func parseDuckDuckGo(page string) []model.Evidence {
	links := ddgResult.FindAllStringSubmatch(page, -1)
	snippets := ddgSnippet.FindAllStringSubmatch(page, -1)
	var out []model.Evidence
	for i, m := range links {
		target := resolveDDGLink(html.UnescapeString(m[1]))
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		if strings.HasSuffix(u.Host, "duckduckgo.com") { // ads
			continue
		}
		title := cleanHTML(m[2])
		snippet := ""
		if i < len(snippets) {
			snippet = cleanHTML(snippets[i][1])
		}
		out = append(out, model.Evidence{
			ID:             fmt.Sprintf("ddg-%d", len(out)),
			Content:        strings.TrimSpace(title + ": " + snippet),
			Source:         u.Host,
			URL:            u.String(),
			AuthorityScore: Authority(u.Host),
			FreshnessScore: Freshness(snippet, time.Now()),
		})
		if len(out) == maxResultCount {
			break
		}
	}
	return out
}

func resolveDDGLink(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if strings.HasSuffix(u.Host, "duckduckgo.com") && strings.HasPrefix(u.Path, "/l/") {
		if target := u.Query().Get("uddg"); target != "" {
			return target
		}
	}
	return href
}

func (s *service) searchWikipedia(ctx context.Context, query string) ([]model.Evidence, error) {
	resp, err := s.get(ctx, "https://en.wikipedia.org/w/api.php?action=query&list=search&format=json&srlimit=5&srsearch="+url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikipedia returned %d", resp.StatusCode)
	}
	var data struct {
		Query struct {
			Search []struct {
				Title     string `json:"title"`
				Snippet   string `json:"snippet"`
				PageID    int    `json:"pageid"`
				Timestamp string `json:"timestamp"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPageBytes)).Decode(&data); err != nil {
		return nil, err
	}
	var out []model.Evidence
	for _, item := range data.Query.Search {
		fresh := 0.9
		if t, err := time.Parse(time.RFC3339, item.Timestamp); err == nil {
			fresh = freshnessFromAge(time.Since(t))
		}
		out = append(out, model.Evidence{
			ID:             fmt.Sprintf("wiki-%d", item.PageID),
			Content:        item.Title + ": " + cleanHTML(item.Snippet),
			Source:         "en.wikipedia.org",
			URL:            "https://en.wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(item.Title, " ", "_")),
			AuthorityScore: 1.0,
			FreshnessScore: fresh,
		})
	}
	return out, nil
}

// FetchPageContent downloads an http(s) page through the SSRF-safe client
// and returns its visible text, capped in size.
func (s *service) FetchPageContent(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("unsupported URL %q", rawURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("URLs with credentials are not fetched")
	}
	resp, err := s.get(ctx, u.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: status %d", u.Host, resp.StatusCode)
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mt != "" && mt != "text/html" && mt != "text/plain" && mt != "application/xhtml+xml" {
		return "", fmt.Errorf("fetch %s: unsupported content type %s", u.Host, mt)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return "", err
	}
	text := string(body)
	if mt != "text/plain" {
		text = cleanHTML(text)
	}
	return util.TruncateRunes(text, maxPageText), nil
}

func cleanHTML(s string) string {
	s = reComment.ReplaceAllString(s, " ")
	s = reScript.ReplaceAllString(s, " ")
	s = reTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// Authority scores a host by a small allow-list of reference sites.
func Authority(host string) float64 {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	if score, ok := authorityTiers[host]; ok {
		return score
	}
	for domain, score := range authorityTiers {
		if strings.HasSuffix(host, "."+domain) {
			return score
		}
	}
	if strings.HasSuffix(host, ".gov") || strings.HasSuffix(host, ".edu") {
		return 0.9
	}
	return 0.4
}

// Freshness scores the first date found in text: 1.0 for today, falling
// linearly to 0.1 at five years old; 0.7 when no date is found.
func Freshness(text string, now time.Time) float64 {
	match := dateRegex.FindString(text)
	if match == "" {
		return 0.7
	}
	t, ok := parseDate(match)
	if !ok {
		return 0.7
	}
	return freshnessFromAge(now.Sub(t))
}

func parseDate(s string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	clean := strings.NewReplacer(",", "", ".", "").Replace(s)
	for _, layout := range []string{"Jan 2 2006", "January 2 2006"} {
		if t, err := time.Parse(layout, clean); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func freshnessFromAge(age time.Duration) float64 {
	years := age.Hours() / 24 / 365
	score := 1.0 - years/5.0
	return min(max(score, 0.1), 1.0)
}
