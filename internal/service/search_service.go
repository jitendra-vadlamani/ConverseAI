package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ai-chat/internal/model"
)

var (
	dateRegex = regexp.MustCompile(`(\b\d{4}-\d{2}-\d{2}\b|\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2},?\s+\d{4}\b)`)
	
	authorityTiers = map[string]float64{
		"github.com":           1.0,
		"docs.microsoft.com":   1.0,
		"developer.mozilla.org": 1.0,
		"en.wikipedia.org":     1.0,
		"aws.amazon.com":       1.0,
		"google.com":           1.0,
		"stackoverflow.com":    0.9,
		"stackexchange.com":    0.9,
		"reddit.com":           0.6,
		"medium.com":           0.6,
		"dev.to":               0.8,
	}
)

type SearchService interface {
	SearchDuckDuckGo(ctx context.Context, query string) ([]model.Evidence, error)
	SearchWikipedia(ctx context.Context, query string) ([]model.Evidence, error)
	FetchPageContent(ctx context.Context, urlStr string) (string, error)
}

type searchService struct {
	client *http.Client
}

func NewSearchService() SearchService {
	return &searchService{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *searchService) SearchDuckDuckGo(ctx context.Context, query string) ([]model.Evidence, error) {
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, _ := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DDG returned status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Simple Regex-based result extraction from html.duckduckgo.com
	// Each result is in a <div class="result__body">
	re := regexp.MustCompile(`<a class="result__a" href="([^"]+)">([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(html, 5) // Limit to top 5

	var evidences []model.Evidence
	for i, m := range matches {
		u, _ := url.QueryUnescape(strings.TrimPrefix(m[1], "//duckduckgo.com/l/?u="))
		uStr := strings.Split(u, "&")[0] // Clean DDG redirect wrapper
		parsedURL, _ := url.Parse(uStr)
		host := ""
		if parsedURL != nil {
			host = parsedURL.Host
		}

		evidences = append(evidences, model.Evidence{
			ID:             fmt.Sprintf("ddg-%d", i),
			Content:        m[2],
			Source:         host,
			URL:            uStr,
			AuthorityScore: s.calculateAuthority(host),
			FreshnessScore: s.extractFreshness(m[2]),
		})
	}

	return evidences, nil
}

func (s *searchService) SearchWikipedia(ctx context.Context, query string) ([]model.Evidence, error) {
	wikiURL := fmt.Sprintf("https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch=%s&format=json&origin=*", url.QueryEscape(query))
	req, _ := http.NewRequestWithContext(ctx, "GET", wikiURL, nil)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
				PageID  int    `json:"pageid"`
			} `json:"search"`
		} `json:"query"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var evidences []model.Evidence
	for _, item := range data.Query.Search {
		snippet := regexp.MustCompile("<[^>]*>").ReplaceAllString(item.Snippet, "")
		evidences = append(evidences, model.Evidence{
			ID:             fmt.Sprintf("wiki-%d", item.PageID),
			Content:        fmt.Sprintf("%s: %s", item.Title, snippet),
			Source:         "Wikipedia",
			URL:            fmt.Sprintf("https://en.wikipedia.org/wiki/%s", url.PathEscape(item.Title)),
			AuthorityScore: 1.0,
			FreshnessScore: 0.9, // Wikipedia is usually fresh enough or evergreen
		})
	}

	return evidences, nil
}

func (s *searchService) FetchPageContent(ctx context.Context, urlStr string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AI-Bot/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch page: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	// Very basic HTML to Text conversion (strip non-body tags and scripts)
	reScript := regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	reStyle := regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	reTags := regexp.MustCompile(`<[^>]*>`)

	text = reScript.ReplaceAllString(text, "")
	text = reStyle.ReplaceAllString(text, "")
	text = reTags.ReplaceAllString(text, " ")
	
	// Normalize whitespace
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	
	if len(text) > 10000 {
		text = text[:10000] // Cap content size
	}

	return strings.Join(strings.Fields(text), " "), nil
}

func (s *searchService) calculateAuthority(host string) float64 {
	host = strings.TrimPrefix(host, "www.")
	if score, ok := authorityTiers[host]; ok {
		return score
	}
	// Default for unknown domains
	return 0.4
}

func (s *searchService) extractFreshness(text string) float64 {
	match := dateRegex.FindString(text)
	if match == "" {
		return 0.7 // Unknown freshness
	}

	// Try to parse the date
	var parsed time.Time
	var err error
	if strings.Contains(match, "-") {
		parsed, err = time.Parse("2006-01-02", match)
	} else {
		// Handle "Jan 02, 2006" style (simplified)
		parsed, err = time.Parse("Jan 02, 2006", strings.ReplaceAll(match, ",", ""))
	}

	if err != nil {
		return 0.7
	}

	years := time.Since(parsed).Hours() / 24 / 365
	score := 1.0 - (years / 5.0)
	if score < 0.1 {
		score = 0.1
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}
