package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/patch/fsutil"
)

func newTurnRunner(t *testing.T, opts RunnerOptions) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	opts.DryRun = true
	r, err := NewRunner(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, root
}

// Two turns on one runner stage into overlays of their own: neither sees
// the other's edits, and the default turn sees neither (ARCH-5: every turn
// staged into the one overlay, so a second session's turn wiped the first).
func TestTurn_StagingIsPerTurn(t *testing.T) {
	r, root := newTurnRunner(t, RunnerOptions{})
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := fsutil.ComputeSHA256([]byte("disk\n"))
	ta := r.NewTurn(TurnOptions{DryRun: true, SessionID: "a"})
	defer ta.Close()
	tb := r.NewTurn(TurnOptions{DryRun: true, SessionID: "b"})
	defer tb.Close()
	ctxA, ctxB := WithTurn(context.Background(), ta), WithTurn(context.Background(), tb)

	if _, err := r.FSWrite(ctxA, FSWriteRequest{Path: "a.txt", Content: "from a\n", FileHash: hash}); err != nil {
		t.Fatalf("write in turn a: %v", err)
	}
	if _, err := r.FSWrite(ctxB, FSWriteRequest{Path: "a.txt", Content: "from b\n", FileHash: hash}); err != nil {
		t.Fatalf("write in turn b: %v", err)
	}
	readIn := func(ctx context.Context) string {
		t.Helper()
		resp, err := r.FSRead(ctx, FSReadRequest{Path: "a.txt"})
		if err != nil {
			t.Fatal(err)
		}
		return resp.Content
	}
	// read numbers the lines; the content is what matters here.
	if got := readIn(ctxA); !strings.Contains(got, "from a") || strings.Contains(got, "from b") {
		t.Fatalf("turn a reads %q", got)
	}
	if got := readIn(ctxB); !strings.Contains(got, "from b") || strings.Contains(got, "from a") {
		t.Fatalf("turn b reads %q", got)
	}
	if got := readIn(context.Background()); !strings.Contains(got, "disk") || strings.Contains(got, "from") {
		t.Fatalf("the default turn reads %q, want the disk", got)
	}
	if r.HasStagedChanges() {
		t.Fatal("the default turn has staged edits it never made")
	}
	if !ta.HasStagedChanges() || !tb.HasStagedChanges() {
		t.Fatal("a turn lost its staged edit")
	}
	// A session's next message starts clean; the other session keeps its edit.
	ta.Begin(false, "a", memory.DefaultConfig())
	if ta.HasStagedChanges() {
		t.Fatal("Begin kept the previous message's staged edits")
	}
	if got := readIn(ctxB); !strings.Contains(got, "from b") {
		t.Fatalf("turn b lost its edit when turn a began a message: %q", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(data) != "disk\n" {
		t.Fatalf("disk changed in a preview: %q", data)
	}
}

// The language servers' view covers every live turn: what any turn staged
// is what they are shown, and a closed turn's drafts vanish from it.
func TestRunner_LanguageServerViewCoversEveryLiveTurn(t *testing.T) {
	r, root := newTurnRunner(t, RunnerOptions{})
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := fsutil.ComputeSHA256([]byte("disk\n"))
	tb := r.NewTurn(TurnOptions{DryRun: true})
	ctxB := WithTurn(context.Background(), tb)
	if _, err := r.FSWrite(ctxB, FSWriteRequest{Path: "b.txt", Content: "new\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.FSWrite(context.Background(), FSWriteRequest{Path: "a.txt", Content: "default\n", FileHash: hash}); err != nil {
		t.Fatal(err)
	}
	paths := strings.Join(r.ListStagedPaths(), ",")
	if !strings.Contains(paths, "a.txt") || !strings.Contains(paths, "b.txt") {
		t.Fatalf("staged paths = %q, want both turns'", paths)
	}
	if content, ok := r.EffectiveContent("b.txt"); !ok || content != "new\n" {
		t.Fatalf("EffectiveContent(b.txt) = %q, %v", content, ok)
	}
	tb.Close()
	if _, ok := r.EffectiveContent("b.txt"); ok {
		t.Fatal("a closed turn's draft is still shown to the language servers")
	}
	if strings.Contains(strings.Join(r.ListStagedPaths(), ","), "b.txt") {
		t.Fatal("a closed turn's path is still listed as staged")
	}
}

// The command gate is the turn's: a preview refuses commands while a turn
// that applies runs them, on the same runner at the same time.
func TestTurn_ExecGateIsPerTurn(t *testing.T) {
	r, _ := newTurnRunner(t, RunnerOptions{BlockExecInDryRun: true})
	preview := r.NewTurn(TurnOptions{DryRun: true})
	defer preview.Close()
	applying := r.NewTurn(TurnOptions{DryRun: true, Apply: true})
	defer applying.Close()
	if !r.ExecRefusedByDryRunIn(WithTurn(context.Background(), preview)) {
		t.Fatal("a preview turn runs commands")
	}
	if r.ExecRefusedByDryRunIn(WithTurn(context.Background(), applying)) {
		t.Fatal("a turn that applies refuses commands")
	}
	if !r.ExecRefusedByDryRun() {
		t.Fatal("the default turn (a dry run) runs commands")
	}
	applying.End()
	if !r.ExecRefusedByDryRunIn(WithTurn(context.Background(), applying)) {
		t.Fatal("after End the turn still runs commands")
	}
}

// A note goes to the memory of the turn's session, not to whichever
// session the runner last heard of.
func TestTurn_SessionMemoryIsPerTurn(t *testing.T) {
	r, root := newTurnRunner(t, RunnerOptions{})
	cfg := memory.DefaultConfig()
	cfg.SessionEnabled = true
	ta := r.NewTurn(TurnOptions{DryRun: true, SessionID: "sess-a", Memory: cfg})
	defer ta.Close()
	tb := r.NewTurn(TurnOptions{DryRun: true, SessionID: "sess-b", Memory: cfg})
	defer tb.Close()
	if err := r.AppendSessionMemory(WithTurn(context.Background(), ta), "note for a"); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendSessionMemory(WithTurn(context.Background(), tb), "note for b"); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "sessions", "sess-a.md"))
	b, _ := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "sessions", "sess-b.md"))
	if !strings.Contains(string(a), "note for a") || strings.Contains(string(a), "note for b") {
		t.Fatalf("session a's memory: %q", a)
	}
	if !strings.Contains(string(b), "note for b") || strings.Contains(string(b), "note for a") {
		t.Fatalf("session b's memory: %q", b)
	}
}
