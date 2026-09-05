package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Remote MCP servers speak Streamable HTTP rather than stdio. The protocol
// side of that — era detection, session ids, SSE framing, reconnects, version
// negotiation across five spec revisions — is delegated to the official SDK;
// this file is the adapter that makes such a server look like the stdio
// Client the Manager already knows how to drive.

// RemoteConfig describes one remote MCP server.
type RemoteConfig struct {
	Name string
	URL  string
	// BearerTokenEnv names the environment variable holding the token sent as
	// `Authorization: Bearer …`. The token itself never lives in the config
	// file, which is committed.
	BearerTokenEnv string
	Headers        map[string]string
}

// RemoteClient is an MCP server reached over Streamable HTTP.
type RemoteClient struct {
	name    string
	session *mcpsdk.ClientSession

	mu           sync.Mutex
	tools        []MCPTool
	allowedTools []string
	lastErr      string

	callTimeout time.Duration
	done        chan struct{}
}

// authTransport attaches the bearer token and any static headers.
type authTransport struct {
	base    http.RoundTripper
	token   string
	headers map[string]string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone: RoundTrippers must not mutate the caller's request.
	r := req.Clone(req.Context())
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// StartRemote connects to a remote MCP server and lists its tools.
func StartRemote(ctx context.Context, cfg RemoteConfig, opts StartOptions) (*RemoteClient, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, fmt.Errorf("mcp remote: server name is required")
	}
	endpoint := strings.TrimSpace(cfg.URL)
	if endpoint == "" {
		return nil, fmt.Errorf("mcp remote %q: url is required", name)
	}

	var token string
	if env := strings.TrimSpace(cfg.BearerTokenEnv); env != "" {
		token = strings.TrimSpace(os.Getenv(env))
		if token == "" {
			return nil, fmt.Errorf("mcp remote %q: bearer_token_env %q is empty or unset", name, env)
		}
	}

	httpClient := &http.Client{
		Transport: &authTransport{token: token, headers: cfg.Headers},
		// Never carry the token to a different host across a redirect.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "orchestra", Version: "vnext"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp remote %q: connect %s: %w", name, endpoint, err)
	}

	c := &RemoteClient{
		name:        name,
		session:     session,
		callTimeout: opts.CallTimeout,
		done:        make(chan struct{}),
	}
	if err := c.refreshTools(ctx); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("mcp remote %q: tools/list: %w", name, err)
	}

	// Wait returns when the connection closes, from either side.
	go func() {
		_ = session.Wait()
		close(c.done)
	}()

	return c, nil
}

func (c *RemoteClient) refreshTools(ctx context.Context) error {
	res, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	tools := make([]MCPTool, 0, len(res.Tools))
	for _, t := range res.Tools {
		if t == nil {
			continue
		}
		var schema json.RawMessage
		if t.InputSchema != nil {
			if raw, mErr := json.Marshal(t.InputSchema); mErr == nil {
				schema = raw
			}
		}
		tools = append(tools, MCPTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	c.mu.Lock()
	c.tools = tools
	c.mu.Unlock()
	return nil
}

// SetAllowedTools restricts which tools this server exposes.
func (c *RemoteClient) SetAllowedTools(names []string) {
	c.mu.Lock()
	c.allowedTools = names
	c.mu.Unlock()
}

// Tools returns the advertised tools, filtered by the per-server allowlist.
func (c *RemoteClient) Tools() []MCPTool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.allowedTools) == 0 {
		return c.tools
	}
	out := make([]MCPTool, 0, len(c.tools))
	for _, t := range c.tools {
		if toolNameAllowed(c.allowedTools, t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// AllToolNames returns every discovered tool, ignoring the allowlist, so the
// settings UI can offer to re-enable one.
func (c *RemoteClient) AllToolNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, t.Name)
	}
	return out
}

// ServerName returns the configured name of this server.
func (c *RemoteClient) ServerName() string { return c.name }

// IsDead reports whether the session has closed.
func (c *RemoteClient) IsDead() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// StderrTail returns the last connection error. A remote server has no stderr;
// the Manager uses this for the same purpose — explaining why it went away.
func (c *RemoteClient) StderrTail() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Call invokes a tool and returns the combined text output.
func (c *RemoteClient) Call(ctx context.Context, toolName string, arguments json.RawMessage) (string, error) {
	text, _, err := c.CallRich(ctx, toolName, arguments)
	return text, err
}

// CallRich is Call plus any images the server returned. Callers that can show
// the model a picture use this; Call stays for the ones that cannot.
func (c *RemoteClient) CallRich(ctx context.Context, toolName string, arguments json.RawMessage) (string, []MCPImage, error) {
	c.mu.Lock()
	allowed := c.allowedTools
	c.mu.Unlock()
	if !toolNameAllowed(allowed, toolName) {
		return "", nil, fmt.Errorf("mcp tool %q is not in this server's allowed_tools list", toolName)
	}
	if c.callTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.callTimeout)
		defer cancel()
	}

	var args any
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", nil, fmt.Errorf("mcp tool %q: arguments are not valid JSON: %w", toolName, err)
		}
	}

	res, err := c.session.CallTool(ctx, &mcpsdk.CallToolParams{Name: toolName, Arguments: args})
	if err != nil {
		c.mu.Lock()
		c.lastErr = err.Error()
		c.mu.Unlock()
		return "", nil, err
	}

	var out strings.Builder
	var images []MCPImage
	dropped := 0
	for _, item := range res.Content {
		switch v := item.(type) {
		case *mcpsdk.TextContent:
			out.WriteString(v.Text)
		case *mcpsdk.ImageContent:
			// The SDK hands this one over already decoded, unlike the stdio
			// path where it arrives as a base64 string.
			if len(v.Data) == 0 {
				dropped++
				continue
			}
			mime := strings.TrimSpace(v.MIMEType)
			if mime == "" {
				mime = "image/png"
			}
			images = append(images, MCPImage{Data: v.Data, MIME: mime})
		default:
			dropped++
		}
	}
	// Same contract as the stdio client: say what could not be carried rather
	// than let the model assume the text was the whole answer.
	if dropped > 0 {
		fmt.Fprintf(&out, "\n[orchestra: dropped %d non-text content item(s); text and images are forwarded, other kinds are not]", dropped)
	}
	if res.IsError {
		return "", nil, fmt.Errorf("mcp tool error: %s", out.String())
	}
	return out.String(), images, nil
}

// Close ends the session.
func (c *RemoteClient) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Close()
}

// toolNameAllowed reports whether a bare tool name passes an allowlist.
// Empty allowlist means everything is allowed. Patterns use path.Match globs.
func toolNameAllowed(allowed []string, name string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, pat := range allowed {
		if pat == name {
			return true
		}
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
}

// ListResources implements resourceServer over Streamable HTTP. A server that
// declines the optional resources capability yields no resources rather than
// an error.
func (c *RemoteClient) ListResources(ctx context.Context) ([]MCPResource, error) {
	res, err := c.session.ListResources(ctx, &mcpsdk.ListResourcesParams{})
	if err != nil {
		if isMethodNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]MCPResource, 0, len(res.Resources))
	for _, r := range res.Resources {
		if r == nil {
			continue
		}
		out = append(out, MCPResource{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MIMEType:    r.MIMEType,
		})
	}
	return out, nil
}

// ReadResource implements resourceServer over Streamable HTTP.
func (c *RemoteClient) ReadResource(ctx context.Context, uri string) (string, error) {
	res, err := c.session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	binary := 0
	for _, ct := range res.Contents {
		if ct == nil {
			continue
		}
		if ct.Text != "" {
			b.WriteString(ct.Text)
			continue
		}
		if len(ct.Blob) > 0 {
			binary++
		}
	}
	if binary > 0 {
		fmt.Fprintf(&b, "\n[orchestra: %d binary resource part(s) omitted]", binary)
	}
	return b.String(), nil
}
