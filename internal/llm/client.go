package llm

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

// Client is an interface for LLM clients
type Client interface {
	Complete(ctx context.Context, prompt string) (string, error)
	Plan(ctx context.Context, prompt string) (string, error) // Returns JSON plan
}

// OpenAIClient is an OpenAI-compatible LLM client
type OpenAIClient struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAIClient creates a new OpenAI-compatible client
func NewOpenAIClient(cfg config.LLMConfig) *OpenAIClient {
	return &OpenAIClient{
		baseURL: cfg.APIBase,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// chatCompletionRequest represents OpenAI chat completion request
type chatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletionResponse represents OpenAI chat completion response
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete generates a response from LLM
func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (string, error) {
	// Ensure baseURL ends with /v1
	baseURL := strings.TrimSuffix(c.baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("api_base is empty")
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL = baseURL + "/v1"
	}
	url := baseURL + "/chat/completions"

	reqBody := chatCompletionRequest{
		Model: c.model,
		Messages: []message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to parse error message for better diagnostics
		var errorResp struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error.Message != "" {
			return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, errorResp.Error.Message)
		}
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp chatCompletionResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error.Message != "" {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return apiResp.Choices[0].Message.Content, nil
}

// Plan generates a plan from LLM (same API as Complete, but with different prompt expectations)
func (c *OpenAIClient) Plan(ctx context.Context, prompt string) (string, error) {
	// For now, use the same Complete method
	// The difference is in the prompt structure and expected response format
	return c.Complete(ctx, prompt)
}
