package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// modelsDialTimeout bounds the TCP connect of a models probe. The probe is
// made for every configured provider each time the settings panel opens, and
// a provider whose host is down — a VPN that is not up — answers no SYN at
// all: the default dialer then sits on the OS's retry schedule until the
// client's whole timeout fires. A live server accepts a connection in
// milliseconds, however slow its model is; only a dead one is cut short here.
const modelsDialTimeout = 3 * time.Second

// modelsTransport carries every models probe. A variable so a test can stand
// in a transport that fails the way a dead host does and count the attempts.
var modelsTransport http.RoundTripper = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   modelsDialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   10 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConns:          8,
	MaxIdleConnsPerHost:   4,
	ExpectContinueTimeout: 1 * time.Second,
	ForceAttemptHTTP2:     true,
}

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
	for _, endpoint := range candidates {
		models, err := fetchModelsURL(ctx, endpoint, cfg.APIKey)
		if err == nil {
			return models, nil
		}
		lastErr = err
		// The candidates are two paths on one host. Not reaching the host at
		// all — a dial that times out or is refused, a TLS failure, the
		// caller's context gone — fails the second path the same way, so
		// trying it only doubles the wait. An HTTP answer is different: a 404
		// on /v1/models says the server is there and the path is wrong.
		// http.Client.Do wraps every transport-level failure in *url.Error;
		// the status and decode errors below are this file's own.
		var transportErr *url.Error
		if errors.As(err, &transportErr) {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no models endpoint tried")
	}
	return nil, lastErr
}

func fetchModelsURL(ctx context.Context, endpoint, apiKey string) ([]RemoteModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: modelsTransport}
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
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", endpoint, resp.StatusCode, truncateForErr(string(body), 200))
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
