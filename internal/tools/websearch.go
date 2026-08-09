package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/orchestra/orchestra/protocol"
)

const (
	tavilyDefaultEndpoint = "https://api.tavily.com/search"
	braveDefaultEndpoint  = "https://api.search.brave.com/res/v1/web/search"
	webSearchTimeout      = 20 * time.Second
	webSearchMaxBytes     = 64 * 1024
)

// WebSearchRequest is the input for the websearch tool.
type WebSearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// WebSearchResult is a single search result.
type WebSearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score,omitempty"`
}

// WebSearchResponse is the output of the websearch tool.
type WebSearchResponse struct {
	Query   string            `json:"query"`
	Results []WebSearchResult `json:"results"`
}

// WebSearch performs a web search using the configured provider (Tavily or Brave).
func (r *Runner) WebSearch(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query is empty", nil)
	}
	cfg := r.webSearchCfg
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return nil, protocol.NewError(protocol.ExecFailed,
			"web.search not configured: set web.search.provider and web.search.api_key in .orchestra.yml", nil)
	}
	if cfg.APIKey == "" {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("web.search.api_key not set for provider %q in .orchestra.yml", provider), nil)
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = cfg.MaxResults
	}
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}
	tctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()

	// Use SSRF-safe dialer in production; bypass it when test endpoint overrides are set.
	var transport http.RoundTripper
	if r.webSearchTavilyEndpoint == "" && r.webSearchBraveEndpoint == "" {
		transport = &http.Transport{DialContext: ssrfSafeDialer()}
	} else {
		transport = http.DefaultTransport
	}
	client := &http.Client{
		Timeout:   webSearchTimeout,
		Transport: transport,
	}
	switch provider {
	case "tavily":
		return r.searchTavily(tctx, client, query, maxResults)
	case "brave":
		return r.searchBrave(tctx, client, query, maxResults)
	default:
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			fmt.Sprintf("unknown web.search.provider %q — supported: tavily, brave", provider), nil)
	}
}

func (r *Runner) searchTavily(ctx context.Context, client *http.Client, query string, maxResults int) (*WebSearchResponse, error) {
	endpoint := r.webSearchTavilyEndpoint
	if endpoint == "" {
		endpoint = tavilyDefaultEndpoint
	}
	body, _ := json.Marshal(map[string]any{
		"api_key":             r.webSearchCfg.APIKey,
		"query":               query,
		"search_depth":        "basic",
		"max_results":         maxResults,
		"include_raw_content": false,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Orchestra/1.0")
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, fmt.Sprintf("tavily request failed: %s", err), nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return nil, protocol.NewError(protocol.ExecFailed, "tavily: invalid API key (HTTP 401)", nil)
	}
	if resp.StatusCode >= 400 {
		return nil, protocol.NewError(protocol.ExecFailed, fmt.Sprintf("tavily: HTTP %d", resp.StatusCode), nil)
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, webSearchMaxBytes))
	if readErr != nil {
		return nil, fmt.Errorf("tavily: read response: %w", readErr)
	}
	var apiResp struct {
		Query   string `json:"query"`
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("tavily: parse response: %w", err)
	}
	results := make([]WebSearchResult, 0, len(apiResp.Results))
	for _, item := range apiResp.Results {
		results = append(results, WebSearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Content,
			Score:   item.Score,
		})
	}
	return &WebSearchResponse{Query: query, Results: results}, nil
}

func (r *Runner) searchBrave(ctx context.Context, client *http.Client, query string, maxResults int) (*WebSearchResponse, error) {
	endpoint := r.webSearchBraveEndpoint
	if endpoint == "" {
		endpoint = braveDefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("brave: parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", maxResults))
	u.RawQuery = q.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("brave: build request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-Subscription-Token", r.webSearchCfg.APIKey)
	httpReq.Header.Set("User-Agent", "Orchestra/1.0")
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, fmt.Sprintf("brave request failed: %s", err), nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return nil, protocol.NewError(protocol.ExecFailed, "brave: invalid API key (HTTP 401)", nil)
	}
	if resp.StatusCode >= 400 {
		return nil, protocol.NewError(protocol.ExecFailed, fmt.Sprintf("brave: HTTP %d", resp.StatusCode), nil)
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, webSearchMaxBytes))
	if readErr != nil {
		return nil, fmt.Errorf("brave: read response: %w", readErr)
	}
	var apiResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("brave: parse response: %w", err)
	}
	results := make([]WebSearchResult, 0, len(apiResp.Web.Results))
	for _, item := range apiResp.Web.Results {
		results = append(results, WebSearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Description,
		})
	}
	return &WebSearchResponse{Query: query, Results: results}, nil
}
