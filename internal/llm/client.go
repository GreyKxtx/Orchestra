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

// truncateID truncates an ID string for logging
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen] + "..."
}

// Client is an interface for LLM clients
type Client interface {
	Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error)
	Plan(ctx context.Context, prompt string) (string, error) // Returns JSON plan (legacy)
}

// OpenAIClient is an OpenAI-compatible LLM client
type OpenAIClient struct {
	baseURL     string
	apiKey      string
	model       string
	maxTokens   int
	temperature float32
	client      *http.Client
	logger      *Logger
}

// NewOpenAIClient creates a new OpenAI-compatible client
func NewOpenAIClient(cfg config.LLMConfig) *OpenAIClient {
	timeout := 60 * time.Second
	if cfg.TimeoutS > 0 {
		timeout = time.Duration(cfg.TimeoutS) * time.Second
	}
	return &OpenAIClient{
		baseURL:     cfg.APIBase,
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		maxTokens:   cfg.MaxTokens,
		temperature: cfg.Temperature,
		client: &http.Client{
			Timeout: timeout,
		},
		logger: nil, // Set via SetLogger if needed
	}
}

// SetLogger sets the logger for this client (optional)
func (c *OpenAIClient) SetLogger(logger *Logger) {
	c.logger = logger
}

// chatCompletionRequest represents OpenAI chat completion request
type chatCompletionRequest struct {
	Model          string               `json:"model"`
	Messages       []Message            `json:"messages"`
	Tools          []ToolDef            `json:"tools,omitempty"`
	ToolChoice     string               `json:"tool_choice,omitempty"`
	MaxTokens      int                  `json:"max_tokens,omitempty"`
	Temperature    *float32             `json:"temperature,omitempty"`
	ResponseFormat *responseFormatWire  `json:"response_format,omitempty"`
}

type responseFormatWire struct {
	Type       string          `json:"type"`
	JSONSchema *jsonSchemaSpec `json:"json_schema,omitempty"`
}

type jsonSchemaSpec struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

// chatCompletionResponse represents OpenAI chat completion response
type chatCompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete generates a single assistant turn (content and/or tool calls).
func (c *OpenAIClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	// Ensure baseURL ends with /v1
	baseURL := strings.TrimSuffix(c.baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("api_base is empty")
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL = baseURL + "/v1"
	}
	url := baseURL + "/chat/completions"

	reqBody := chatCompletionRequest{
		Model:     c.model,
		Messages:  req.Messages,
		MaxTokens: c.maxTokens,
		Tools:     req.Tools,
	}
	if c.temperature != 0 {
		temp := c.temperature
		reqBody.Temperature = &temp
	}
	// If tools are provided, let the model choose automatically.
	if len(reqBody.Tools) > 0 {
		reqBody.ToolChoice = "auto"
	}
	if req.ResponseFormat != nil && req.ResponseFormat.Type != "" {
		wf := &responseFormatWire{Type: req.ResponseFormat.Type}
		if req.ResponseFormat.Type == "json_schema" && len(req.ResponseFormat.Schema) > 0 {
			name := req.ResponseFormat.SchemaName
			if name == "" {
				name = "response"
			}
			wf.JSONSchema = &jsonSchemaSpec{
				Name:   name,
				Schema: json.RawMessage(req.ResponseFormat.Schema),
				Strict: true,
			}
		}
		reqBody.ResponseFormat = wf
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	requestBytes := len(jsonData)
	requestPreview := string(jsonData) // Will be sanitized in logger

	// Extract message roles for logging
	messageRoles := make([]string, 0, len(reqBody.Messages))
	for _, msg := range reqBody.Messages {
		roleStr := string(msg.Role)
		// Add tool_calls info for assistant messages
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			roleStr = fmt.Sprintf("%s(tool_calls=%d)", roleStr, len(msg.ToolCalls))
		}
		// Add tool_call_id info for tool messages
		if msg.Role == RoleTool && msg.ToolCallID != "" {
			roleStr = fmt.Sprintf("%s(id=%s)", roleStr, truncateID(msg.ToolCallID, 12))
		}
		messageRoles = append(messageRoles, roleStr)
	}

	// Log request
	startTime := time.Now()
	if c.logger != nil {
		c.logger.LogRequest(url, c.model, int(c.client.Timeout.Seconds()), requestBytes, len(reqBody.Tools), len(reqBody.Messages), messageRoles, requestPreview)
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	reqHTTP.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		reqHTTP.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(reqHTTP)
	duration := time.Since(startTime)
	if err != nil {
		if c.logger != nil {
			c.logger.LogError(0, err.Error(), duration.Milliseconds())
		}
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if c.logger != nil {
			c.logger.LogError(resp.StatusCode, fmt.Sprintf("failed to read response: %v", err), duration.Milliseconds())
		}
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	responseBytes := len(body)
	responsePreview := string(body)

	if resp.StatusCode != http.StatusOK {
		// Log error
		if c.logger != nil {
			c.logger.LogError(resp.StatusCode, responsePreview, duration.Milliseconds())
		}
		// Try to parse error message for better diagnostics
		var errorResp struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error.Message != "" {
			return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, errorResp.Error.Message)
		}
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Log successful response
	if c.logger != nil {
		c.logger.LogResponse(responseBytes, duration.Milliseconds(), responsePreview)
	}

	var apiResp chatCompletionResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &CompleteResponse{Message: apiResp.Choices[0].Message}, nil
}

// Plan generates a plan from LLM (same API as Complete, but with different prompt expectations)
func (c *OpenAIClient) Plan(ctx context.Context, prompt string) (string, error) {
	resp, err := c.Complete(ctx, CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}
