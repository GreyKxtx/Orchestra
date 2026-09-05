package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// MCPPrompt is a named, argument-taking recipe a server offers to a person —
// the server's own "slash command". Unlike a tool, it is not something the
// model decides to call; it is something the user picks.
type MCPPrompt struct {
	Server      string         `json:"server"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Arguments   []MCPPromptArg `json:"arguments,omitempty"`
}

// MCPPromptArg is one argument a prompt accepts.
type MCPPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// promptServer is the optional prompts half of a ServerClient. prompts/* is
// optional in MCP, so a server without it must not break the rest.
type promptServer interface {
	ListPrompts(ctx context.Context) ([]MCPPrompt, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (string, error)
}

// ListPrompts returns every prompt across all servers, each tagged with the
// server that offers it. Asked once per server, like resources: this feeds
// the command palette, which is refreshed far more often than it changes.
func (m *Manager) ListPrompts(ctx context.Context) []MCPPrompt {
	if m.IsEmpty() {
		return nil
	}
	var out []MCPPrompt
	for _, c := range m.clients {
		for _, p := range m.cachedPrompts(ctx, c) {
			p.Server = c.ServerName()
			out = append(out, p)
		}
	}
	return out
}

func (m *Manager) cachedPrompts(ctx context.Context, c ServerClient) []MCPPrompt {
	name := c.ServerName()

	m.mu.Lock()
	if m.promptCache != nil {
		if items, done := m.promptCache[name]; done {
			m.mu.Unlock()
			return items
		}
	}
	m.mu.Unlock()

	ps, ok := c.(promptServer)
	if !ok {
		return nil
	}
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := ps.ListPrompts(listCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp: server %q prompts/list: %v\n", name, err)
		items = nil
	}

	m.mu.Lock()
	if m.promptCache == nil {
		m.promptCache = map[string][]MCPPrompt{}
	}
	m.promptCache[name] = items
	m.mu.Unlock()
	return items
}

// GetPrompt renders one prompt into the text to send as the user's turn.
func (m *Manager) GetPrompt(ctx context.Context, server, name string, args map[string]string) (string, error) {
	c := m.findClient(strings.TrimSpace(server))
	if c == nil {
		return "", fmt.Errorf("mcp server %q not found", server)
	}
	ps, ok := c.(promptServer)
	if !ok {
		return "", fmt.Errorf("mcp server %q does not offer prompts", server)
	}
	return ps.GetPrompt(ctx, strings.TrimSpace(name), args)
}

// promptMessagesToText flattens a prompts/get result into the text to send.
//
// A prompt is a list of messages, but Orchestra sends it as one user turn:
// the alternative is injecting a synthetic conversation the model never had,
// which reads worse in the transcript and in the session file. Non-text parts
// are counted rather than dropped in silence, like everywhere else here.
func promptMessagesToText(raw json.RawMessage) (string, error) {
	var res struct {
		Description string `json:"description"`
		Messages    []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("prompts/get: %w", err)
	}
	var b strings.Builder
	omitted := 0
	for _, msg := range res.Messages {
		if msg.Content.Type != "text" || msg.Content.Text == "" {
			omitted++
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(msg.Content.Text)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "\n\n[orchestra: %d non-text prompt part(s) omitted]", omitted)
	}
	return b.String(), nil
}

// --- stdio transport ---

// ListPrompts implements promptServer over stdio.
func (c *Client) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	raw, err := c.call(ctx, "prompts/list", nil)
	if err != nil {
		if isMethodNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var res struct {
		Prompts []MCPPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("prompts/list: %w", err)
	}
	return res.Prompts, nil
}

// GetPrompt implements promptServer over stdio.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	raw, err := c.call(ctx, "prompts/get", params)
	if err != nil {
		return "", err
	}
	return promptMessagesToText(raw)
}
