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

func TestAutoMemoryNote_Explore(t *testing.T) {
	in := json.RawMessage(`{"symbol_name":"Agent.Run"}`)
	note := AutoMemoryNote("explore", in, "[digest]\n- location: agent.go:403-520\n")
	if !strings.Contains(note, "Agent.Run") {
		t.Fatalf("note: %q", note)
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
