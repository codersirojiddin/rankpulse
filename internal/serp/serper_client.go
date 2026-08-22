// Package serp wraps the Serper.dev Google Search API and provides
// the rank-matching + persistence logic used by the daily worker.
package serp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const serperEndpoint = "https://google.serper.dev/search"

type Client struct {
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type searchRequest struct {
	Q   string `json:"q"`
	Gl  string `json:"gl"`  // target country code, e.g. "us"
	Num int    `json:"num"` // number of organic results to return
}

type OrganicResult struct {
	Title    string `json:"title"`
	Link     string `json:"link"`
	Position int    `json:"position"`
}

type searchResponse struct {
	Organic []OrganicResult `json:"organic"`
}

// Search queries Serper.dev for the given keyword within the given
// country and returns the raw organic results (up to 100).
func (c *Client) Search(ctx context.Context, keyword, countryCode string) ([]OrganicResult, error) {
	reqBody := searchRequest{
		Q:   keyword,
		Gl:  countryCode,
		Num: 100,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serperEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serper request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serper returned status %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode serper response: %w", err)
	}

	return sr.Organic, nil
}

// FindRank scans organic results for the first entry whose link
// belongs to the target domain and returns its position, or nil if
// the domain doesn't appear in the returned results.
func FindRank(results []OrganicResult, targetDomain string) *int {
	target := normalizeDomain(targetDomain)

	for _, r := range results {
		resultDomain := normalizeDomain(r.Link)
		if resultDomain == target || strings.HasSuffix(resultDomain, "."+target) {
			pos := r.Position
			return &pos
		}
	}
	return nil
}

func normalizeDomain(raw string) string {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimPrefix(d, "www.")
	if idx := strings.IndexAny(d, "/?#"); idx != -1 {
		d = d[:idx]
	}
	return d
}
