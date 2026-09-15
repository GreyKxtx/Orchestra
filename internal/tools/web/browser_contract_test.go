package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"

	"github.com/orchestra/orchestra/internal/browser"
)

const pinnedSchemasPath = "testdata/playwright-mcp-tools.json"

type pinnedSchemas struct {
	Package string                     `json:"package"`
	Tools   map[string]json.RawMessage `json:"tools"`
}

func loadPinnedToolSchemas(t *testing.T) pinnedSchemas {
	t.Helper()
	raw, err := os.ReadFile(pinnedSchemasPath)
	if err != nil {
		t.Fatalf("read %s: %v", pinnedSchemasPath, err)
	}
	var s pinnedSchemas
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parse %s: %v", pinnedSchemasPath, err)
	}
	return s
}

// validateAgainst returns the schema violations of args for tool, or "" when
// the pinned server would accept them.
func validateAgainst(s pinnedSchemas, tool string, args json.RawMessage) string {
	schema, ok := s.Tools[tool]
	if !ok {
		return "tool " + tool + " is not offered by " + s.Package
	}
	var doc map[string]any
	_ = json.Unmarshal(schema, &doc)
	// Recorded as draft 2020-12; the keywords these schemas use are the same in
	// the draft gojsonschema implements.
	delete(doc, "$schema")
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage(`{}`)
	}
	res, err := gojsonschema.Validate(gojsonschema.NewGoLoader(doc), gojsonschema.NewBytesLoader(args))
	if err != nil {
		return err.Error()
	}
	var problems []string
	for _, e := range res.Errors() {
		problems = append(problems, e.String())
	}
	return strings.Join(problems, "\n")
}

func TestBrowserClient_RunsTheServerVersionTheSchemasWereRecordedFrom(t *testing.T) {
	s := loadPinnedToolSchemas(t)
	if browser.PlaywrightMCPPackage != s.Package {
		t.Fatalf("the client starts %q but the argument mapping is checked against %q: "+
			"re-record %s from the pinned version when changing it",
			browser.PlaywrightMCPPackage, s.Package, pinnedSchemasPath)
	}
}

// Every request shape the model can send, through the functions the tool
// dispatch calls, must reach the pinned server as arguments it accepts.
func TestBrowserTools_SendArgumentsThePinnedServerAccepts(t *testing.T) {
	cfg := newMockBrowserConfig(t, true)
	ctx := context.Background()
	calls := []struct {
		name string
		run  func() error
	}{
		{"navigate", func() error {
			_, err := BrowserNavigate(ctx, cfg, BrowserNavigateRequest{URL: "http://127.0.0.1:1/"})
			return err
		}},
		{"navigate wait_until=domcontentloaded", func() error {
			_, err := BrowserNavigate(ctx, cfg, BrowserNavigateRequest{URL: "http://127.0.0.1:1/", WaitUntil: "domcontentloaded"})
			return err
		}},
		{"navigate wait_until=networkidle", func() error {
			_, err := BrowserNavigate(ctx, cfg, BrowserNavigateRequest{URL: "http://127.0.0.1:1/", WaitUntil: "networkidle"})
			return err
		}},
		{"snapshot", func() error { _, err := BrowserSnapshot(ctx, cfg, BrowserSnapshotRequest{}); return err }},
		{"screenshot", func() error { _, err := BrowserScreenshot(ctx, cfg, BrowserScreenshotRequest{}); return err }},
		{"screenshot full page", func() error {
			_, err := BrowserScreenshot(ctx, cfg, BrowserScreenshotRequest{FullPage: true})
			return err
		}},
		{"click by ref", func() error { _, err := BrowserClick(ctx, cfg, BrowserClickRequest{Ref: "e9"}); return err }},
		{"click by selector", func() error {
			_, err := BrowserClick(ctx, cfg, BrowserClickRequest{Element: "#go"})
			return err
		}},
		{"type by ref", func() error {
			_, err := BrowserType(ctx, cfg, BrowserTypeRequest{Ref: "e4", Text: "Ada", Clear: true})
			return err
		}},
		{"type by selector", func() error {
			_, err := BrowserType(ctx, cfg, BrowserTypeRequest{Element: "#name", Text: "Ada"})
			return err
		}},
		{"fill", func() error {
			_, err := BrowserFill(ctx, cfg, BrowserFillRequest{Fields: []BrowserFillField{
				{Ref: "e4", Value: "Ada"}, {Element: "#email", Value: "ada@example.com"},
			}})
			return err
		}},
		{"select", func() error {
			_, err := BrowserSelect(ctx, cfg, BrowserSelectRequest{Ref: "e6", Value: "pro"})
			return err
		}},
		{"eval", func() error {
			_, err := BrowserEval(ctx, cfg, BrowserEvalRequest{Expression: "document.title"})
			return err
		}},
		{"wait text", func() error { _, err := BrowserWait(ctx, cfg, BrowserWaitRequest{Text: "Created"}); return err }},
		{"wait selector", func() error {
			_, err := BrowserWait(ctx, cfg, BrowserWaitRequest{Selector: "#out"})
			return err
		}},
		{"wait url", func() error {
			_, err := BrowserWait(ctx, cfg, BrowserWaitRequest{URL: "/done"})
			return err
		}},
		{"close", func() error { _, err := BrowserClose(ctx, cfg, BrowserCloseRequest{}); return err }},
	}
	for _, c := range calls {
		if err := c.run(); err != nil {
			t.Errorf("browser %s: %v", c.name, err)
		}
	}
}

// The server can only wait for text or a fixed time. Waiting for a selector or
// a URL is what the tool promises, so it is done by asking the page, without
// the allow_eval gate that exists for the model's own JavaScript.
func TestBrowserWait_ForSelectorOrURLAsksThePageAndGivesUpWithTheCondition(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "calls.log")
	cfg := newMockBrowserConfig(t, false, "BE_BROWSER_LOG="+logPath)
	if _, err := BrowserWait(context.Background(), cfg, BrowserWaitRequest{Selector: `#out[data-x="1"]`, URL: "/done"}); err != nil {
		t.Fatalf("wait with allow_eval off: %v", err)
	}
	logged, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logged), "browser_evaluate") ||
		!strings.Contains(string(logged), `#out[data-x=\\\"1\\\"]`) || !strings.Contains(string(logged), "/done") {
		t.Errorf("the page was not asked about the selector and the URL:\n%s", logged)
	}

	never := newMockBrowserConfig(t, false, "BE_BROWSER_EVAL_RESULT=false")
	_, err := BrowserWait(context.Background(), never, BrowserWaitRequest{Selector: "#out", TimeoutMS: 600})
	if err == nil {
		t.Fatal("a condition that never holds was reported as met")
	}
	if !strings.Contains(err.Error(), "#out") || !strings.Contains(err.Error(), "600") {
		t.Errorf("the timeout does not say what was waited for and how long: %v", err)
	}
}
