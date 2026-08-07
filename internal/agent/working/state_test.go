package working

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestState_ObserveAndFormat(t *testing.T) {
	s := New("fix the handler")
	s.ObserveTool("read", json.RawMessage(`{"path":"a.go"}`), []byte(`{"content":"x"}`), nil)
	s.ObserveTool("edit", json.RawMessage(`{"path":"a.go"}`), []byte(`{"diagnostics":[{"severity":"error","message":"undefined: Foo"}]}`), nil)
	s.SetTodos([]TodoView{{Content: "fix tests", Status: "pending"}, {Content: "done item", Status: "completed"}})

	ws := s.FormatWorkingState()
	for _, want := range []string{"<working_state>", "goal:", "a.go", "edit×1", "undefined: Foo", "fix tests"} {
		if !strings.Contains(ws, want) {
			t.Fatalf("working_state missing %q:\n%s", want, ws)
		}
	}
	files := s.ActiveFiles()
	if len(files) != 1 || files[0] != "a.go" {
		t.Fatalf("ActiveFiles: %v", files)
	}
	dig := s.BuildTurnDigest(0)
	if !strings.Contains(dig, "[turn_digest]") || !strings.Contains(dig, "edit a.go") {
		t.Fatalf("digest: %s", dig)
	}
	micro := s.BuildTurnDigest(4)
	if !strings.Contains(micro, "step: 4") {
		t.Fatalf("micro digest missing step: %s", micro)
	}
}

func TestPersistAndLoadDigests(t *testing.T) {
	root := t.TempDir()
	sid := "sess1"
	s := New("ship feature")
	s.ObserveTool("write", json.RawMessage(`{"path":"b.ts"}`), []byte(`{}`), nil)
	if err := PersistTurnDigest(root, sid, s.BuildTurnDigest(0)); err != nil {
		t.Fatal(err)
	}
	// second digest
	s2 := New("follow-up")
	s2.ObserveTool("read", json.RawMessage(`{"path":"c.go"}`), nil, nil)
	_ = PersistTurnDigest(root, sid, s2.BuildTurnDigest(0))

	path := filepath.Join(root, ".orchestra", "memory", "sessions", sid+".turns.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	block := FormatRecentTurnDigests(root, sid, 3)
	if !strings.Contains(block, "<turn_digests>") || !strings.Contains(block, "ship feature") {
		t.Fatalf("inject block: %s", block)
	}
}

func TestPersistTurnDigest_TrimsOld(t *testing.T) {
	root := t.TempDir()
	sid := "trim"
	for i := 0; i < 30; i++ {
		s := New(fmt.Sprintf("g%d", i))
		if err := PersistTurnDigest(root, sid, s.BuildTurnDigest(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, ".orchestra", "memory", "sessions", sid+".turns.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(string(data), "[turn_digest]")
	if n != maxStoredTurnDigests {
		t.Fatalf("kept %d digests, want %d", n, maxStoredTurnDigests)
	}
	if strings.Contains(string(data), "goal: g0") {
		t.Fatal("oldest digests should be trimmed")
	}
}
