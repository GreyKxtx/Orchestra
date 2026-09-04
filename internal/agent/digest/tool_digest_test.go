package digest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDigestToolOutput_Explore(t *testing.T) {
	raw := []byte(`{"content":"# internal/agent/agent.go:403-520\n\n**Вызывается из (callers):**\n- ` + "`core.SessionMessage`" + ` (core.go)\n\nfunc Run() {\n  // compaction here\n` + strings.Repeat("x", 5000) + `"}`)
	in := json.RawMessage(`{"symbol_name":"Agent.Run"}`)
	out, digested := DigestToolOutput("explore", in, raw, 800)
	if !digested {
		t.Fatal("expected digest")
	}
	if !strings.Contains(out, "Agent.Run") {
		t.Fatalf("missing symbol: %s", out)
	}
	if !strings.Contains(out, "callers") {
		t.Fatalf("missing callers: %s", out)
	}
}

func TestDigestToolOutput_SmallPassthrough(t *testing.T) {
	raw := []byte(`{"ok":true}`)
	out, digested := DigestToolOutput("read", nil, raw, 2048)
	if digested || out != string(raw) {
		t.Fatalf("unexpected digest: %q", out)
	}
}

func TestDigestToolOutput_PreservesErrorShape(t *testing.T) {
	// Same shape agent.formatToolErrorJSON produces for a failed "bash" call.
	errMsg := strings.Repeat("compile failed: undefined symbol foo ", 200)
	raw, _ := json.Marshal(map[string]any{
		"status": "error",
		"tool":   "bash",
		"code":   "TOOL_ERROR",
		"error":  errMsg,
		"input":  map[string]any{"command": "go build ./..."},
	})
	out, digested := DigestToolOutput("bash", json.RawMessage(`{"command":"go build ./..."}`), raw, 200)
	if !digested {
		t.Fatal("expected digest (raw exceeds budget)")
	}
	if !strings.Contains(out, "status: error") {
		t.Fatalf("expected preserved status, got: %s", out)
	}
	if !strings.Contains(out, "code: TOOL_ERROR") {
		t.Fatalf("expected preserved code, got: %s", out)
	}
	if !strings.Contains(out, "undefined symbol foo") {
		t.Fatalf("expected preserved error text, got: %s", out)
	}
}

func TestDigestToolOutput_PreservesDeniedShape(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"status": "denied",
		"tool":   "write",
		"reason": strings.Repeat("ask mode is read-only; blocked write to protected path ", 20),
		"input":  map[string]any{"path": "a.go"},
	})
	out, digested := DigestToolOutput("write", json.RawMessage(`{"path":"a.go"}`), raw, 100)
	if !digested {
		t.Fatal("expected digest (raw exceeds budget)")
	}
	if !strings.Contains(out, "status: denied") || !strings.Contains(out, "ask mode is read-only") {
		t.Fatalf("expected preserved denial reason, got: %s", out)
	}
}

func TestAutoMemoryNote_Explore(t *testing.T) {
	in := json.RawMessage(`{"symbol_name":"Agent.Run"}`)
	note := AutoMemoryNote("explore", in, "[digest]\n- location: agent.go:403-520\n")
	if !strings.Contains(note, "Agent.Run") {
		t.Fatalf("note: %q", note)
	}
}

func TestAutoMemoryNote_GrepMatchCountIsNotAFact(t *testing.T) {
	in := json.RawMessage(`{"query":"className"}`)

	note := AutoMemoryNote("grep", in, "[digest]\n- a.jsx:1\n- b.jsx:2\n")

	// A match count is a property of one search, not a durable fact about the
	// project. Writing these filled 5 of 6 session memory files in the field
	// run with lines like `grep "className=" — 31 match lines in digest`, and
	// that noise is injected back as [session] memory on every later step.
	if note != "" {
		t.Fatalf("grep must not write session memory, got %q", note)
	}
}

func TestDigestToolOutput_Grep(t *testing.T) {
	var b strings.Builder
	b.WriteString("Найдено 50 совпадений для \"foo\":\n\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "internal/pkg/file%d.go:%d  [в: bar]  call\n>   foo()\n", i, i*10)
	}
	raw, _ := json.Marshal(b.String())
	out, digested := DigestToolOutput("grep", json.RawMessage(`{"query":"foo"}`), raw, 400)
	if !digested {
		t.Fatal("expected digest")
	}
	if !strings.Contains(out, "matches") {
		t.Fatalf("bad grep digest: %s", out)
	}
}
