# Agent Tools — `web.search` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `web.search` tool that lets the Orchestra agent search the web and get back structured results. Gated by `allowWeb` (same flag as `webfetch`). Supports two providers: **Tavily** (default, AI-first API) and **Brave** — configured in `.orchestra.yml`.

**Architecture:** Add `search` sub-config under the existing `WebConfig` in `internal/config/config.go`. Implement `WebSearch()` in `internal/tools/websearch.go` following the same HTTP-client pattern as `webfetch.go`. Provider is selected at request time from `Runner.webSearchConfig`. No new dependencies — uses `encoding/json` + `net/http` (already in use by webfetch).

**Tech Stack:** Go standard library (`net/http`, `encoding/json`, `context`), existing `internal/protocol`, `internal/tools` patterns. Zero new Go modules.

---

## File Map

| File | Change |
|------|--------|
| `internal/config/config.go` | **Modify**: add `WebSearchConfig` struct + `Search WebSearchConfig` field to `WebConfig` |
| `internal/tools/runner.go` | **Modify**: add `webSearchCfg WebSearchConfig` field to `Runner`, populate in `NewRunner` |
| `internal/tools/websearch.go` | **Create**: `WebSearch()` implementation + Request/Response types |
| `internal/tools/websearch_test.go` | **Create**: tests (mock HTTP server, no real API calls) |
| `internal/tools/registry.go` | **Modify**: add `toolWebSearch()` def + add to `ListTools()` when `allowWeb` |
| `internal/tools/call.go` | **Modify**: add `"websearch"` dispatch case |
| `internal/config/config.go` | **Modify**: add `"websearch"` to `validAgentToolNames` |
| `internal/protocol/version.go` | **Modify**: bump `ToolsVersion` (if web.search plan executed standalone) |

> **Note:** If this plan is executed after the `2026-05-16-agent-tools-git-fs.md` plan, `ToolsVersion` should already be 7. Bump it to 8. If executed independently (before the git/fs plan), bump from 6 to 7.

---

### Task 1: Add `WebSearchConfig` to config

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Write a failing config test**

Append to `internal/config/config_test.go`:

```go
func TestWebSearchConfig_DefaultProvider(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()
	// No provider configured → empty string is valid (tool will error at call time).
	if cfg.Web.Search.Provider != "" {
		t.Errorf("expected empty default provider, got %q", cfg.Web.Search.Provider)
	}
}

func TestWebSearchConfig_YAML(t *testing.T) {
	raw := `
web:
  search:
    provider: tavily
    api_key: tvly-test123
    max_results: 10
`
	cfg, err := loadFromBytes([]byte(raw))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Web.Search.Provider != "tavily" {
		t.Errorf("provider=%q, want tavily", cfg.Web.Search.Provider)
	}
	if cfg.Web.Search.APIKey != "tvly-test123" {
		t.Errorf("api_key=%q, want tvly-test123", cfg.Web.Search.APIKey)
	}
	if cfg.Web.Search.MaxResults != 10 {
		t.Errorf("max_results=%d, want 10", cfg.Web.Search.MaxResults)
	}
}
```

**Note:** Check if `loadFromBytes` helper exists in `config_test.go`. If it doesn't exist, use the pattern from other tests in that file to load config from a YAML string.

- [ ] **Step 2: Run to verify it fails**

```
go test ./internal/config/ -run "TestWebSearchConfig" -v
```

Expected: FAIL — `WebSearchConfig` field undefined, or `loadFromBytes` not found (adapt test to existing helper pattern in that file first).

- [ ] **Step 3: Add `WebSearchConfig` to `config.go`**

In `internal/config/config.go`, add this struct (after `WebConfig`):

```go
// WebSearchConfig configures the web.search tool provider.
// Supported providers: "tavily", "brave".
// Provider-specific docs:
//   - Tavily:  https://docs.tavily.com/docs/rest-api/api-reference  (free tier: 1000 req/month)
//   - Brave:   https://brave.com/search/api/  (free tier: 2000 req/month)
type WebSearchConfig struct {
	// Provider selects the search API: "tavily" (default when APIKey set) or "brave".
	Provider string `yaml:"provider,omitempty"`
	// APIKey is the provider API key (required).
	APIKey string `yaml:"api_key,omitempty"`
	// MaxResults limits results per query. Default 5.
	MaxResults int `yaml:"max_results,omitempty"`
}
```

Add `Search WebSearchConfig` field to `WebConfig`:

```go
type WebConfig struct {
	Confirm         *bool           `yaml:"confirm"`
	FetchTimeoutS   int             `yaml:"fetch_timeout_s"`
	MaxContentBytes int             `yaml:"max_content_bytes"`
	Search          WebSearchConfig `yaml:"search,omitempty"`
}
```

Add `"websearch"` to `validAgentToolNames`:

```go
var validAgentToolNames = map[string]bool{
	// ... existing entries ...
	"websearch": true,
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/config/ -run "TestWebSearchConfig" -v
```

Expected: PASS.

- [ ] **Step 5: Build check**

```
go build ./...
```

- [ ] **Step 6: Commit**

```
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add WebSearchConfig (provider, api_key, max_results)"
```

---

### Task 2: Thread `WebSearchConfig` through `Runner`

**Files:**
- Modify: `internal/tools/runner.go`

- [ ] **Step 1: Check `RunnerOptions` and `NewRunner` signature**

Read `internal/tools/runner.go` and find:
- The `RunnerOptions` struct
- The `Runner` struct fields
- How `NewRunner` populates runner from options

This tells you exactly where to add the new field.

- [ ] **Step 2: Add field to `Runner` and `RunnerOptions`**

In `Runner` struct, add:

```go
webSearchCfg config.WebSearchConfig
```

Import `"github.com/orchestra/orchestra/internal/config"` if not already imported.

In `RunnerOptions` struct, add:

```go
WebSearch config.WebSearchConfig
```

In `NewRunner` body, populate:

```go
r.webSearchCfg = opts.WebSearch
```

- [ ] **Step 3: Thread config from agent/CLI callers**

Search for all callers of `NewRunner` or `RunnerOptions{}`:

```
grep -rn "RunnerOptions" internal/ cmd/ --include="*.go"
```

For each caller that already passes `WebConfig` or `WebFetchTimeout`, add the `WebSearch` field:

```go
RunnerOptions{
    // ... existing fields ...
    WebSearch: cfg.Web.Search,
}
```

- [ ] **Step 4: Build check**

```
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```
git add internal/tools/runner.go
git commit -m "feat(tools): thread WebSearchConfig into Runner"
```

---

### Task 3: Implement `web.search` tool

**Files:**
- Create: `internal/tools/websearch.go`
- Create: `internal/tools/websearch_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/call.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tools/websearch_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func newWebSearchRunner(t *testing.T, cfg config.WebSearchConfig) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{WebSearch: cfg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestWebSearch_Tavily_Basic(t *testing.T) {
	// Mock Tavily server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "golang context" {
			t.Errorf("unexpected query: %v", body["query"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": "golang context",
			"results": []map[string]any{
				{"title": "Go Context", "url": "https://pkg.go.dev/context", "content": "Package context...", "score": 0.95},
			},
		})
	}))
	defer srv.Close()

	r, _ := newWebSearchRunner(t, config.WebSearchConfig{
		Provider:   "tavily",
		APIKey:     "test-key",
		MaxResults: 5,
	})
	// Override endpoint to mock server.
	r.webSearchCfg.tavilyEndpoint = srv.URL

	resp, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "golang context"})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Go Context" {
		t.Errorf("unexpected title: %q", resp.Results[0].Title)
	}
	if resp.Results[0].URL != "https://pkg.go.dev/context" {
		t.Errorf("unexpected url: %q", resp.Results[0].URL)
	}
}

func TestWebSearch_NoProviderConfigured(t *testing.T) {
	r, _ := newWebSearchRunner(t, config.WebSearchConfig{}) // no provider
	_, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error when no provider configured")
	}
}

func TestWebSearch_EmptyQueryFails(t *testing.T) {
	r, _ := newWebSearchRunner(t, config.WebSearchConfig{Provider: "tavily", APIKey: "k"})
	_, err := r.WebSearch(context.Background(), WebSearchRequest{Query: ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestWebSearch_Brave_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") == "" {
			t.Error("missing X-Subscription-Token header")
		}
		if r.URL.Query().Get("q") != "rust ownership" {
			t.Errorf("unexpected query: %q", r.URL.Query().Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"web": map[string]any{
				"results": []map[string]any{
					{"title": "Rust Ownership", "url": "https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html", "description": "What is Ownership?"},
				},
			},
		})
	}))
	defer srv.Close()

	r, _ := newWebSearchRunner(t, config.WebSearchConfig{
		Provider:   "brave",
		APIKey:     "brave-test-key",
		MaxResults: 5,
	})
	r.webSearchCfg.braveEndpoint = srv.URL

	resp, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "rust ownership"})
	if err != nil {
		t.Fatalf("WebSearch brave: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Rust Ownership" {
		t.Errorf("unexpected title: %q", resp.Results[0].Title)
	}
}
```

**Note:** The test uses `r.webSearchCfg.tavilyEndpoint` and `r.webSearchCfg.braveEndpoint` — we'll add these as internal override fields to `WebSearchConfig` in the next step.

- [ ] **Step 2: Run to verify it fails**

```
go test ./internal/tools/ -run TestWebSearch -v
```

Expected: FAIL — `WebSearch`, `WebSearchRequest` undefined; `tavilyEndpoint`/`braveEndpoint` fields missing.

- [ ] **Step 3: Add endpoint override fields to `WebSearchConfig`**

In `internal/config/config.go`, add to `WebSearchConfig` (lowercase = unexported, yaml-skipped):

```go
type WebSearchConfig struct {
	Provider   string `yaml:"provider,omitempty"`
	APIKey     string `yaml:"api_key,omitempty"`
	MaxResults int    `yaml:"max_results,omitempty"`

	// Override endpoints for testing. Not set from YAML (lowercase tags).
	tavilyEndpoint string
	braveEndpoint  string
}
```

- [ ] **Step 4: Implement `WebSearch`**

Create `internal/tools/websearch.go`:

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
)

const (
	tavilyDefaultEndpoint = "https://api.tavily.com/search"
	braveDefaultEndpoint  = "https://api.search.brave.com/res/v1/web/search"
	webSearchTimeout      = 20 * time.Second
	webSearchMaxBytes     = 64 * 1024 // 64 KB response cap
)

// WebSearchRequest is the input for the websearch tool.
type WebSearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"` // override config default
}

// WebSearchResult is one search hit.
type WebSearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score,omitempty"`
}

// WebSearchResponse is the output of the websearch tool.
type WebSearchResponse struct {
	Query   string            `json:"query"`
	Results []WebSearchResult `json:"results"`
}

// WebSearch performs a web search using the configured provider (Tavily or Brave).
func (r *Runner) WebSearch(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query is empty", nil)
	}

	cfg := r.webSearchCfg
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return nil, protocol.NewError(protocol.ExecFailed,
			"web.search not configured: set web.search.provider and web.search.api_key in .orchestra.yml",
			nil)
	}
	if cfg.APIKey == "" {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("web.search.api_key not set for provider %q in .orchestra.yml", provider), nil)
	}

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = cfg.MaxResults
	}
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}

	tctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()

	client := &http.Client{
		Timeout:   webSearchTimeout,
		Transport: &http.Transport{DialContext: ssrfSafeDialer()},
	}

	switch provider {
	case "tavily":
		return r.searchTavily(tctx, client, query, maxResults)
	case "brave":
		return r.searchBrave(tctx, client, query, maxResults)
	default:
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			fmt.Sprintf("unknown web.search.provider %q — supported: tavily, brave", provider), nil)
	}
}

// searchTavily calls the Tavily Search API.
func (r *Runner) searchTavily(ctx context.Context, client *http.Client, query string, maxResults int) (*WebSearchResponse, error) {
	endpoint := r.webSearchCfg.tavilyEndpoint
	if endpoint == "" {
		endpoint = tavilyDefaultEndpoint
	}

	body, _ := json.Marshal(map[string]any{
		"api_key":             r.webSearchCfg.APIKey,
		"query":               query,
		"search_depth":        "basic",
		"max_results":         maxResults,
		"include_raw_content": false,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Orchestra/1.0")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, fmt.Sprintf("tavily request failed: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return nil, protocol.NewError(protocol.ExecFailed, "tavily: invalid API key (HTTP 401)", nil)
	}
	if resp.StatusCode >= 400 {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("tavily: HTTP %d", resp.StatusCode), nil)
	}

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, webSearchMaxBytes))
	if readErr != nil {
		return nil, fmt.Errorf("tavily: read response: %w", readErr)
	}

	var apiResp struct {
		Query   string `json:"query"`
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("tavily: parse response: %w", err)
	}

	results := make([]WebSearchResult, 0, len(apiResp.Results))
	for _, r := range apiResp.Results {
		results = append(results, WebSearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
			Score:   r.Score,
		})
	}
	return &WebSearchResponse{Query: query, Results: results}, nil
}

// searchBrave calls the Brave Search API.
func (r *Runner) searchBrave(ctx context.Context, client *http.Client, query string, maxResults int) (*WebSearchResponse, error) {
	endpoint := r.webSearchCfg.braveEndpoint
	if endpoint == "" {
		endpoint = braveDefaultEndpoint
	}

	u, _ := url.Parse(endpoint)
	q := u.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", maxResults))
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("brave: build request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-Subscription-Token", r.webSearchCfg.APIKey)
	httpReq.Header.Set("User-Agent", "Orchestra/1.0")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, fmt.Sprintf("brave request failed: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return nil, protocol.NewError(protocol.ExecFailed, "brave: invalid API key (HTTP 401)", nil)
	}
	if resp.StatusCode >= 400 {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("brave: HTTP %d", resp.StatusCode), nil)
	}

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, webSearchMaxBytes))
	if readErr != nil {
		return nil, fmt.Errorf("brave: read response: %w", readErr)
	}

	var apiResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("brave: parse response: %w", err)
	}

	results := make([]WebSearchResult, 0, len(apiResp.Web.Results))
	for _, item := range apiResp.Web.Results {
		results = append(results, WebSearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Description,
		})
	}
	return &WebSearchResponse{Query: query, Results: results}, nil
}
```

- [ ] **Step 5: Add `toolWebSearch()` to `registry.go`**

In `ListTools()`, add inside the `if allowWeb` block:

```go
if allowWeb {
    out = append(out, toolWebFetch())
    out = append(out, toolWebSearch())
}
```

Add to `applyParallelFlags()` `ParallelSafe` case:

```go
case "ls", "read", "glob", "grep", "symbols", "explore",
    "todoread", "task.result", "runtime.query", "webfetch", "websearch",
    "lsp.definition", "lsp.references", "lsp.hover", "lsp.diagnostics",
    "diff.preview",
    "git.status", "git.diff", "git.log":
    defs[i].ParallelSafe = true
```

Add the tool definition function:

```go
func toolWebSearch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "websearch",
			Description: "Поиск в интернете. Возвращает список результатов с заголовком, URL и сниппетом. Требует настройки web.search.provider и web.search.api_key в .orchestra.yml.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query":       { "type": "string", "minLength": 1, "description": "Поисковый запрос." },
    "max_results": { "type": "integer", "minimum": 1, "maximum": 20, "description": "Максимум результатов. По умолчанию из конфига (5)." }
  }
}`),
		},
	}
}
```

- [ ] **Step 6: Add dispatch case to `call.go`**

```go
case "websearch":
    var req WebSearchRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.WebSearch(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)
```

- [ ] **Step 7: Run tests**

```
go test ./internal/tools/ -run TestWebSearch -v
```

Expected: PASS — all 4 tests.

- [ ] **Step 8: Build check**

```
go build ./...
```

- [ ] **Step 9: Commit**

```
git add internal/tools/websearch.go internal/tools/websearch_test.go internal/tools/registry.go internal/tools/call.go
git commit -m "feat(tools): add web.search tool (Tavily + Brave providers)"
```

---

### Task 4: Bump `ToolsVersion` + docs update

**Files:**
- Modify: `internal/protocol/version.go`

- [ ] **Step 1: Bump ToolsVersion**

Check current value in `internal/protocol/version.go`. If `ToolsVersion` is:
- 6 → set to 7 (standalone execution before git/fs plan)
- 7 → set to 8 (executed after git/fs plan)

- [ ] **Step 2: Run full test suite**

```
go test ./...
```

Expected: all tests pass.

- [ ] **Step 3: Build final binary**

```
go build -o orchestra.exe ./cmd/orchestra
```

- [ ] **Step 4: Commit**

```
git add internal/protocol/version.go
git commit -m "feat(tools): bump ToolsVersion; websearch requires allowWeb"
```

---

## Configuration Guide (for users)

Add to `.orchestra.yml` to enable web search:

```yaml
web:
  search:
    provider: tavily        # or "brave"
    api_key: tvly-xxxxxx   # Tavily key from app.tavily.com
    max_results: 5

  # For Brave:
  # search:
  #   provider: brave
  #   api_key: BSAxxxx...   # from api.search.brave.com
  #   max_results: 5
```

Free tier limits:
- **Tavily**: 1000 searches/month at app.tavily.com (free tier)
- **Brave**: 2000 searches/month at api.search.brave.com (free tier)

---

## Self-Review

**Spec coverage:**
- [x] `websearch` tool — Task 3
- [x] Tavily provider — Task 3
- [x] Brave provider — Task 3
- [x] Config (`WebSearchConfig`) — Task 1
- [x] Runner field — Task 2
- [x] `allowWeb` gating — Task 3 (registry.go)
- [x] `ToolsVersion` bump — Task 4
- [x] `validAgentToolNames` — Task 1

**Placeholder scan:** No TBDs. Mock server tests are concrete.

**Type consistency:** `WebSearchConfig.tavilyEndpoint` and `WebSearchConfig.braveEndpoint` are set in tests (`r.webSearchCfg.tavilyEndpoint = srv.URL`) and read in `searchTavily`/`searchBrave` — consistent throughout.
