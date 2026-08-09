// Package lmstudio is a thin client for LM Studio's *server management*
// endpoints (model list, model load) — NOT for LLM chat completions.
//
// Distinct from internal/llm/, which talks to /v1/chat/completions via
// an OpenAI-compatible provider abstraction. This package owns LM
// Studio's product-specific endpoints (/api/v0/models, /api/v1/models/
// load) that have no analogue in other providers. Merging the two
// packages was considered (M1 in architecture audit) and rejected:
// they have different lifecycle, different consumers (this is used by
// the TUI onboarding + model dialog, not by the agent loop), and
// different cardinality (one LM Studio per host, many LLM providers).
package lmstudio

import (
	"bytes"
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
	apiKey     string // optional Bearer token for cloud /v1/models
	httpClient *http.Client
}

// NewClient creates a new OpenAI-compatible models client with a 5-second timeout.
// api_base may include a trailing /v1 (chat completions style); management
// paths are rooted at the host without that suffix.
func NewClient(endpoint, apiKey string) *Client {
	return &Client{
		endpoint:   stripOpenAIV1(endpoint),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func stripOpenAIV1(endpoint string) string {
	e := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if len(e) >= 3 && strings.EqualFold(e[len(e)-3:], "/v1") {
		return strings.TrimRight(e[:len(e)-3], "/")
	}
	return e
}

func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if strings.Contains(strings.ToLower(c.endpoint), "ngrok") {
		req.Header.Set("ngrok-skip-browser-warning", "true")
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
// vLLM exposes max_model_len; LM Studio may use max_context_length / context_length.
type v1Model struct {
	ID               string `json:"id"`
	MaxContextLength int64  `json:"max_context_length"`
	MaxModelLen      int64  `json:"max_model_len"`
	ContextLength    int64  `json:"context_length"`
}

func (m v1Model) resolvedContextLen() int64 {
	for _, n := range []int64{m.MaxModelLen, m.MaxContextLength, m.ContextLength} {
		if n > 0 {
			return n
		}
	}
	return 0
}

type v1Response struct {
	Data []v1Model `json:"data"`
}

// GPUOffloadConfig controls how many model layers run on GPU.
// Ratio "max" offloads all layers; a float in [0,1] offloads that fraction.
type GPUOffloadConfig struct {
	Ratio interface{} `json:"ratio"` // "max" or float64
}

// LoadModelRequest is the body sent to POST /api/v1/models/load.
type LoadModelRequest struct {
	Model         string            `json:"model"`
	ContextLength int               `json:"context_length,omitempty"`
	GPUOffload    *GPUOffloadConfig `json:"gpu_offload,omitempty"`
}

// LoadModelResponse is the LM Studio /api/v1/models/load response.
type LoadModelResponse struct {
	Status     string `json:"status"`
	Type       string `json:"type"`
	LoadConfig *struct {
		ContextLength int `json:"context_length"`
	} `json:"load_config,omitempty"`
}

// IsModelLoaded reports whether modelID is currently loaded in LM Studio.
// Returns false (no error) when the model list cannot be fetched.
func (c *Client) IsModelLoaded(modelID string) bool {
	models, err := c.ListModels()
	if err != nil {
		return false
	}
	for _, m := range models {
		if m.ID == modelID && m.IsLoaded {
			return true
		}
	}
	return false
}

// LoadModel calls POST /api/v1/models/load to ensure the given model is
// loaded with the requested context length and full GPU offload.
// A dedicated HTTP client with a 10-minute timeout is used because large
// models can take minutes to load.
// Returns nil if the server does not support the endpoint (non-LM Studio hosts).
func (c *Client) LoadModel(modelID string, contextLength int) (*LoadModelResponse, error) {
	body, _ := json.Marshal(LoadModelRequest{
		Model:         modelID,
		ContextLength: contextLength,
		GPUOffload:    &GPUOffloadConfig{Ratio: "max"},
	})
	req, err := http.NewRequest(http.MethodPost, c.endpoint+"/api/v1/models/load", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	loadClient := &http.Client{Timeout: 10 * time.Minute}
	resp, err := loadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // endpoint not supported — not an LM Studio host
	}
	if resp.StatusCode == http.StatusBadRequest {
		return nil, nil // model already loaded — LM Studio returns 400 on reload attempt
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("load model returned %d", resp.StatusCode)
	}
	var out LoadModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
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
	req, err := http.NewRequest(http.MethodGet, c.endpoint+"/api/v0/models", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
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
		if m.Object != "model" {
			continue
		}
		// Accept generation-capable models. Older LM Studio versions may
		// omit Type — treat empty as a generation model. Only embeddings
		// are filtered out (they can't generate text).
		if m.Type == "embeddings" {
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
	req, err := http.NewRequest(http.MethodGet, c.endpoint+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
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
			MaxContextLength: m.resolvedContextLen(),
		})
	}
	return result, nil
}

// FindModelContext returns the server-advertised context window for modelID
// (exact or case-insensitive match). Returns 0,nil when the model is listed
// without a context field; error only on transport/decode failures.
func (c *Client) FindModelContext(modelID string) (int64, error) {
	models, err := c.ListModels()
	if err != nil {
		return 0, err
	}
	want := strings.TrimSpace(modelID)
	if want == "" {
		// Prefer the first model that reports a context length.
		for _, m := range models {
			if m.MaxContextLength > 0 {
				return m.MaxContextLength, nil
			}
		}
		return 0, nil
	}
	var fuzzy *RemoteModel
	for i := range models {
		m := &models[i]
		if m.ID == want {
			return m.MaxContextLength, nil
		}
		if strings.EqualFold(m.ID, want) {
			fuzzy = m
		}
	}
	if fuzzy != nil {
		return fuzzy.MaxContextLength, nil
	}
	// Suffix / contains match (vLLM may serve "Qwen/…" while config has "qwen/…").
	low := strings.ToLower(want)
	for i := range models {
		m := &models[i]
		id := strings.ToLower(m.ID)
		if strings.HasSuffix(id, low) || strings.HasSuffix(low, id) || strings.Contains(id, low) {
			if m.MaxContextLength > 0 {
				return m.MaxContextLength, nil
			}
		}
	}
	return 0, nil
}
