// Package embed provides a thin OpenAI-compatible embeddings client.
// Works with OpenAI, Ollama (/v1/embeddings), LM Studio, Voyage, and any
// other server that implements POST /v1/embeddings with {model, input}
// and returns {data: [{embedding: [floats]}]}.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// Client embeds text into vectors. Implementations must be safe for
// concurrent use.
type Client interface {
	// Embed returns one vector per input. The returned slice has the
	// same length as inputs and is in the same order.
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	// Model returns the model identifier used by this client. Stored
	// alongside each embedding so future queries can detect a mismatch.
	Model() string
	// Dim returns the vector dimensionality (0 when not yet known).
	Dim() int
}

// HTTPClient is the OpenAI-compatible implementation.
type HTTPClient struct {
	baseURL    string
	apiKey     string
	model      string
	dim        int
	httpClient *http.Client
	batchSize  int
}

// New constructs a client from EmbedConfig. APIBase defaults to OpenAI's
// embeddings endpoint when empty. BatchSize defaults to 32.
func New(cfg config.EmbedConfig) *HTTPClient {
	base := strings.TrimRight(cfg.APIBase, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = 32
	}
	timeout := time.Duration(cfg.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &HTTPClient{
		baseURL:    base,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		dim:        cfg.Dimensions,
		httpClient: &http.Client{Timeout: timeout},
		batchSize:  batch,
	}
}

// Model returns the configured model identifier.
func (c *HTTPClient) Model() string { return c.model }

// Dim returns the configured (or learned) vector dimensionality.
func (c *HTTPClient) Dim() int { return c.dim }

type embedReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
	} `json:"error,omitempty"`
}

// Embed sends inputs in batches and concatenates the results.
func (c *HTTPClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if c.model == "" {
		return nil, fmt.Errorf("embed: model is empty (set embed.model in .orchestra.yml)")
	}
	out := make([][]float32, 0, len(inputs))
	for i := 0; i < len(inputs); i += c.batchSize {
		end := i + c.batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		vecs, err := c.embedBatch(ctx, inputs[i:end])
		if err != nil {
			return nil, fmt.Errorf("embed batch [%d:%d]: %w", i, end, err)
		}
		out = append(out, vecs...)
	}
	if c.dim == 0 && len(out) > 0 {
		c.dim = len(out[0])
	}
	return out, nil
}

func (c *HTTPClient) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	body, _ := json.Marshal(embedReq{Model: c.model, Input: inputs})
	url := c.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	// Free ngrok tunnels return HTML/401 without this header.
	if strings.Contains(strings.ToLower(c.baseURL), "ngrok") {
		req.Header.Set("ngrok-skip-browser-warning", "true")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embed: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 512))
	}
	var parsed embedResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("embed: parse response: %w; body=%s", err, truncate(string(raw), 256))
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("embed: server error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embed: expected %d vectors, got %d", len(inputs), len(parsed.Data))
	}
	vecs := make([][]float32, len(parsed.Data))
	for i := range parsed.Data {
		vecs[i] = parsed.Data[i].Embedding
	}
	return vecs, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// CosineSimilarity returns cosine similarity in [-1, 1]. Returns 0 when
// either vector is zero-length or dimensions differ. Vectors are assumed
// to be non-normalised; the function does the division.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	// Use float64 for the sqrt+div, cast back.
	return float32(float64(dot) / (sqrtf(na) * sqrtf(nb)))
}

func sqrtf(x float32) float64 {
	// Local helper to avoid pulling in math at the top (and keep this
	// file dependency-light). math.Sqrt accepts float64.
	return _sqrt(float64(x))
}
