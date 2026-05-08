package lmstudio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RemoteModel is a model available on the local LM Studio instance.
type RemoteModel struct {
	ID               string
	MaxContextLength int64
	IsLoaded         bool // true when model is currently loaded in memory
}

// Client talks to a LM Studio (or OpenAI-compatible) local server.
type Client struct {
	endpoint   string // e.g. "http://localhost:1234"
	httpClient *http.Client
}

// NewClient creates a new LM Studio client with a 5-second timeout.
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// v0Model matches the LM Studio beta /api/v0/models response.
type v0Model struct {
	ID               string `json:"id"`
	Object           string `json:"object"`
	Type             string `json:"type"`
	State            string `json:"state"`
	MaxContextLength int64  `json:"max_context_length"`
}

type v0Response struct {
	Data []v0Model `json:"data"`
}

// v1Model matches the OpenAI-compatible /v1/models response.
type v1Model struct {
	ID               string `json:"id"`
	MaxContextLength int64  `json:"max_context_length"` // may be absent
}

type v1Response struct {
	Data []v1Model `json:"data"`
}

// ListModels fetches available models. Tries /api/v0/models first (LM Studio beta),
// falls back to /v1/models (OpenAI-compatible).
func (c *Client) ListModels() ([]RemoteModel, error) {
	models, err := c.listV0()
	if err == nil {
		return models, nil
	}
	return c.listV1()
}

func (c *Client) listV0() ([]RemoteModel, error) {
	resp, err := c.httpClient.Get(c.endpoint + "/api/v0/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("v0 endpoint returned %d", resp.StatusCode)
	}
	var out v0Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var result []RemoteModel
	for _, m := range out.Data {
		if m.Object != "model" || m.Type != "llm" {
			continue
		}
		result = append(result, RemoteModel{
			ID:               m.ID,
			MaxContextLength: m.MaxContextLength,
			IsLoaded:         m.State == "loaded",
		})
	}
	return result, nil
}

func (c *Client) listV1() ([]RemoteModel, error) {
	resp, err := c.httpClient.Get(c.endpoint + "/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("v1 endpoint returned %d", resp.StatusCode)
	}
	var out v1Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	result := make([]RemoteModel, 0, len(out.Data))
	for _, m := range out.Data {
		result = append(result, RemoteModel{
			ID:               m.ID,
			MaxContextLength: m.MaxContextLength,
		})
	}
	return result, nil
}
