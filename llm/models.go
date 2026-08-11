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

// RemoteModel is one entry from OpenAI-compatible GET …/models.
type RemoteModel struct {
	ID               string `json:"id"`
	OwnedBy          string `json:"owned_by,omitempty"`
	MaxModelLen      int64  `json:"max_model_len,omitempty"`
	MaxContextLength int64  `json:"max_context_length,omitempty"`
	ContextLength    int64  `json:"context_length,omitempty"`
}

// ContextTokens returns the server-advertised context window when present.
func (m RemoteModel) ContextTokens() int {
	for _, n := range []int64{m.MaxModelLen, m.MaxContextLength, m.ContextLength} {
		if n > 0 {
			return int(n)
		}
	}
	return 0
}

// ListRemoteModels fetches models from cfg.APIBase (/v1/models or /models).
func ListRemoteModels(ctx context.Context, cfg LLMConfig) ([]RemoteModel, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if base == "" {
		return nil, fmt.Errorf("api_base is empty")
	}
	candidates := []string{base + "/models"}
	if !strings.HasSuffix(base, "/v1") {
		candidates = append([]string{base + "/v1/models"}, candidates...)
	}

	var lastErr error
	for _, url := range candidates {
		models, err := fetchModelsURL(ctx, url, cfg.APIKey)
		if err == nil {
			return models, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no models endpoint tried")
	}
	return nil, lastErr
}

func fetchModelsURL(ctx context.Context, url, apiKey string) ([]RemoteModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, truncateForErr(string(body), 200))
	}
	var payload struct {
		Data []RemoteModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	return payload.Data, nil
}

func truncateForErr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
