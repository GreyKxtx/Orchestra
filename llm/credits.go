package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Credits is the account balance snapshot for providers that expose one.
type Credits struct {
	// TotalCredits is lifetime credits purchased (USD).
	TotalCredits float64 `json:"total_credits"`
	// TotalUsage is lifetime spend (USD).
	TotalUsage float64 `json:"total_usage"`
}

// Balance returns the remaining credits (never negative).
func (c Credits) Balance() float64 {
	b := c.TotalCredits - c.TotalUsage
	if b < 0 {
		return 0
	}
	return b
}

// SupportsCredits reports whether the endpoint exposes a credits/balance API
// we know how to query (currently OpenRouter only).
func SupportsCredits(cfg LLMConfig) bool {
	return strings.Contains(strings.ToLower(cfg.APIBase), "openrouter.ai")
}

// FetchCredits queries the provider's credits endpoint (OpenRouter:
// GET {api_base}/credits). Requires a configured api_key.
func FetchCredits(ctx context.Context, cfg LLMConfig) (*Credits, error) {
	if !SupportsCredits(cfg) {
		return nil, fmt.Errorf("provider does not expose a credits API")
	}
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("api_key is not configured")
	}
	url := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/") + "/credits"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("credits request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("credits request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("credits response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("credits request failed (status %d): %s", resp.StatusCode, truncateAndSanitize(string(body), 256))
	}

	var parsed struct {
		Data Credits `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("credits response: %w", err)
	}
	return &parsed.Data, nil
}
