package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent/working"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestInjectWorkingPromptBlocks(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	a := &Agent{
		tools: tr,
		opts: Options{
			SessionID:      "s1",
			TurnDigestKeep: 3,
		},
		working: working.New("save tokens"),
	}
	a.working.ObserveTool("read", json.RawMessage(`{"path":"x.go"}`), []byte(`{}`), nil)

	wsOn := true
	a.opts.WorkingState = &wsOn
	block := a.injectWorkingPromptBlocks()
	if !strings.Contains(block, "<working_state>") || !strings.Contains(block, "x.go") {
		t.Fatalf("expected working_state, got:\n%s", block)
	}

	_ = working.PersistTurnDigest(root, "s1", a.working.BuildTurnDigest(0))
	block = a.injectWorkingPromptBlocks()
	if !strings.Contains(block, "<turn_digests>") {
		t.Fatalf("expected turn_digests after persist, got:\n%s", block)
	}

	a.opts.TurnDigestKeep = 0
	block = a.injectWorkingPromptBlocks()
	if strings.Contains(block, "<turn_digests>") {
		t.Fatalf("turn digests should be off: %s", block)
	}

	path := filepath.Join(root, ".orchestra", "memory", "sessions", "s1.turns.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestMaybePersistMicroDigest(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	a := &Agent{
		tools: tr,
		opts: Options{
			SessionID:        "micro",
			TurnDigestKeep:   3,
			TurnDigestEveryN: 4,
		},
		working: working.New("micro summary"),
	}
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"a.go"}`), []byte(`{}`), nil)

	a.maybePersistMicroDigest(3) // not multiple
	path := filepath.Join(root, ".orchestra", "memory", "sessions", "micro.turns.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("should not write at step 3: %v", err)
	}

	a.maybePersistMicroDigest(4)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "step: 4") || !strings.Contains(string(data), "a.go") {
		t.Fatalf("micro digest: %s", data)
	}
}

func TestPersistWorkingTurnDigest_RespectsKeep(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	a := &Agent{
		tools: tr,
		opts: Options{
			SessionID:      "s2",
			TurnDigestKeep: 0,
		},
		working: working.New("noop"),
	}
	a.persistWorkingTurnDigest()
	path := filepath.Join(root, ".orchestra", "memory", "sessions", "s2.turns.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("should not persist when keep=0: %v", err)
	}
	a.opts.TurnDigestKeep = 3
	// Goal-only turns produce no digest — nothing happened, no junk file.
	a.persistWorkingTurnDigest()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty turn should not create a digest file: %v", err)
	}
	a.working.ObserveTool("read", json.RawMessage(`{"path":"y.go"}`), []byte(`{}`), nil)
	a.persistWorkingTurnDigest()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
