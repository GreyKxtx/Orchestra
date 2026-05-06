package agent

import (
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestSubjectForTool(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		input   string
		wantSub string
	}{
		{"bash command", "bash", `{"command":"go test ./..."}`, "go test ./..."},
		{"webfetch url", "webfetch", `{"url":"https://pkg.go.dev/fmt"}`, "https://pkg.go.dev/fmt"},
		{"write path", "write", `{"path":"src/main.go","content":"x"}`, "src/main.go"},
		{"edit path", "edit", `{"path":"a.go","search":"x","replace":"y"}`, "a.go"},
		{"read path", "read", `{"path":"b.go"}`, "b.go"},
		{"ls path", "ls", `{"path":"internal/"}`, "internal/"},
		{"grep path", "grep", `{"path":"*.go","pattern":"func"}`, "*.go"},
		{"symbols path", "symbols", `{"path":"foo.go"}`, "foo.go"},
		{"glob pattern", "glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"explore name", "explore", `{"name":"MyFunc"}`, "MyFunc"},
		{"todowrite no subject", "todowrite", `{"todos":[]}`, ""},
		{"memory_write no subject", "memory_write", `{"content":"x"}`, ""},
		{"empty input", "bash", `{}`, ""},
		{"invalid json", "bash", `not-json`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := subjectForTool(c.tool, json.RawMessage(c.input))
			if got != c.wantSub {
				t.Errorf("subjectForTool(%q, %q) = %q, want %q", c.tool, c.input, got, c.wantSub)
			}
		})
	}
}

func TestCheckPermissions(t *testing.T) {
	allow := func(tool, pattern string) config.PermissionRule {
		return config.PermissionRule{Tool: tool, Pattern: pattern, Action: "allow"}
	}
	deny := func(tool, pattern string) config.PermissionRule {
		return config.PermissionRule{Tool: tool, Pattern: pattern, Action: "deny"}
	}

	cases := []struct {
		name        string
		rules       []config.PermissionRule
		tool        string
		subject     string
		wantAction  string
		wantMatched bool
	}{
		{
			name:        "no rules → no match",
			rules:       nil,
			tool:        "bash",
			subject:     "go test ./...",
			wantAction:  "",
			wantMatched: false,
		},
		{
			name:        "wildcard tool allow",
			rules:       []config.PermissionRule{allow("*", "")},
			tool:        "bash",
			subject:     "anything",
			wantAction:  "allow",
			wantMatched: true,
		},
		{
			name:        "exact tool deny no pattern",
			rules:       []config.PermissionRule{deny("bash", "")},
			tool:        "bash",
			subject:     "rm -rf /",
			wantAction:  "deny",
			wantMatched: true,
		},
		{
			name:        "exact tool deny does not match other tools",
			rules:       []config.PermissionRule{deny("bash", "")},
			tool:        "webfetch",
			subject:     "https://example.com",
			wantAction:  "",
			wantMatched: false,
		},
		{
			name: "pattern allow first match wins",
			rules: []config.PermissionRule{
				allow("bash", "go test *"),
				deny("bash", ""),
			},
			tool:        "bash",
			subject:     "go test ./...",
			wantAction:  "allow",
			wantMatched: true,
		},
		{
			name: "pattern deny when not in allowlist",
			rules: []config.PermissionRule{
				allow("bash", "go test *"),
				deny("bash", ""),
			},
			tool:        "bash",
			subject:     "rm -rf /",
			wantAction:  "deny",
			wantMatched: true,
		},
		{
			name:        "pattern no match → no match",
			rules:       []config.PermissionRule{allow("bash", "go build *")},
			tool:        "bash",
			subject:     "rm -rf /",
			wantAction:  "",
			wantMatched: false,
		},
		{
			name:        "file extension pattern",
			rules:       []config.PermissionRule{allow("write", "*.go")},
			tool:        "write",
			subject:     "main.go",
			wantAction:  "allow",
			wantMatched: true,
		},
		{
			name:        "file extension pattern no match",
			rules:       []config.PermissionRule{deny("write", "*.yaml")},
			tool:        "write",
			subject:     "main.go",
			wantAction:  "",
			wantMatched: false,
		},
		{
			name:        "case-insensitive tool name",
			rules:       []config.PermissionRule{allow("BASH", "")},
			tool:        "bash",
			subject:     "go vet",
			wantAction:  "allow",
			wantMatched: true,
		},
		{
			name:        "malformed action skipped",
			rules:       []config.PermissionRule{{Tool: "bash", Pattern: "", Action: "maybe"}},
			tool:        "bash",
			subject:     "",
			wantAction:  "",
			wantMatched: false,
		},
		{
			name: "wildcard deny overrides everything with no pattern",
			rules: []config.PermissionRule{
				deny("*", ""),
			},
			tool:        "read",
			subject:     "main.go",
			wantAction:  "deny",
			wantMatched: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			act, matched := checkPermissions(c.rules, c.tool, c.subject)
			if matched != c.wantMatched {
				t.Errorf("matched: want %v, got %v", c.wantMatched, matched)
			}
			if act != c.wantAction {
				t.Errorf("action: want %q, got %q", c.wantAction, act)
			}
		})
	}
}
