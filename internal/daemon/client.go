package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: baseURL,
		hc: &http.Client{
			Timeout: 1 * time.Second,
		},
	}
}

func NewClientWithToken(baseURL string, token string) *Client {
	c := NewClient(baseURL)
	c.token = strings.TrimSpace(token)
	return c
}

func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.getJSON(ctx, "/api/v1/health", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Refresh(ctx context.Context) (*RefreshResponse, error) {
	var out RefreshResponse
	if err := c.postJSON(ctx, "/api/v1/refresh", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Context(ctx context.Context, req ContextRequest) (*ContextResponse, error) {
	var out ContextResponse
	if err := c.postJSON(ctx, "/api/v1/context", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	var out SearchResponse
	if err := c.postJSON(ctx, "/api/v1/search", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func DiscoverDaemonURL(projectRoot string, cfgDaemonAddress string, cfgDaemonPort int) (string, bool, error) {
	// 1) .orchestra/daemon.json
	if info, ok, err := ReadDiscovery(projectRoot); err == nil && ok {
		if info.URL != "" {
			return info.URL, true, nil
		}
	}

	// 2) env
	if v := strings.TrimSpace(os.Getenv("ORCHESTRA_DAEMON_URL")); v != "" {
		return v, true, nil
	}

	// 3) config values
	if cfgDaemonAddress != "" && cfgDaemonPort != 0 {
		return fmt.Sprintf("http://%s:%d", cfgDaemonAddress, cfgDaemonPort), true, nil
	}

	return "", false, nil
}

// DiscoverDaemonInfo resolves daemon URL + token with priority:
// 1) .orchestra/daemon.json
// 2) env: ORCHESTRA_DAEMON_URL (+ optional ORCHESTRA_DAEMON_TOKEN)
// 3) config values (+ required ORCHESTRA_DAEMON_TOKEN)
func DiscoverDaemonInfo(projectRoot string, cfgDaemonAddress string, cfgDaemonPort int) (*DiscoveryInfo, bool, error) {
	// 1) discovery file
	if info, ok, err := ReadDiscovery(projectRoot); err == nil && ok && info != nil {
		if strings.TrimSpace(info.URL) != "" {
			return info, true, nil
		}
	}

	// 2) env
	if v := strings.TrimSpace(os.Getenv("ORCHESTRA_DAEMON_URL")); v != "" {
		return &DiscoveryInfo{
			ProtocolVersion: ProtocolVersion,
			ProjectRoot:     projectRoot,
			URL:             v,
			Token:           strings.TrimSpace(os.Getenv("ORCHESTRA_DAEMON_TOKEN")),
		}, true, nil
	}

	// 3) config values (require token in env)
	if cfgDaemonAddress != "" && cfgDaemonPort != 0 {
		token := strings.TrimSpace(os.Getenv("ORCHESTRA_DAEMON_TOKEN"))
		if token == "" {
			// Token is mandatory in vNext; avoid guessing URL without a token.
			return nil, false, nil
		}
		return &DiscoveryInfo{
			ProtocolVersion: ProtocolVersion,
			ProjectRoot:     projectRoot,
			URL:             fmt.Sprintf("http://%s:%d", cfgDaemonAddress, cfgDaemonPort),
			Token:           token,
		}, true, nil
	}

	return nil, false, nil
}

func (c *Client) getJSON(ctx context.Context, p string, out any) error {
	url := c.baseURL + path.Clean(p)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func (c *Client) postJSON(ctx context.Context, p string, in any, out any) error {
	url := c.baseURL + path.Clean(p)
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func decodeResponse(resp *http.Response, out any) error {
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// try error response
		var er struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &er) == nil && er.Error != "" {
			return fmt.Errorf("daemon API error (%d): %s", resp.StatusCode, er.Error)
		}
		return fmt.Errorf("daemon API error (%d): %s", resp.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("failed to parse daemon response: %w", err)
	}
	return nil
}
