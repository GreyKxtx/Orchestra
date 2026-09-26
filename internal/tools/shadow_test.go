package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helperCommand is the test binary running one TestExecRun_Helper mode
// against a file, from the working directory the runner picks.
func helperCommand(t *testing.T, mode, file string) ExecRunRequest {
	t.Helper()
	t.Setenv("ORCHESTRA_EXEC_HELPER_MODE", mode)
	t.Setenv("ORCHESTRA_EXEC_HELPER_FILE", file)
	return ExecRunRequest{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestExecRun_Helper$", "-test.count=1"},
	}
}

func newShadowRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(root, RunnerOptions{DryRun: true, BlockExecInDryRun: true, ShadowExec: true, ExcludeDirs: []string{"node_modules"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, root
}

// A preview turn's command runs in a shadow of the workspace that carries
// the turn's staged edits (LLM-11): the model's test runs against the code
// it just changed, not the code on disk — and the disk stays as it was.
func TestShadow_ACommandSeesTheStagedEdit(t *testing.T) {
	r, root := newShadowRunner(t)
	ctx := context.Background()
	hash := r.overlayAt(ctx).CurrentHash("a.txt")
	if _, err := r.FSWrite(ctx, FSWriteRequest{Path: "a.txt", Content: "staged\n", FileHash: hash}); err != nil {
		t.Fatalf("stage a.txt: %v", err)
	}
	resp, err := r.ExecRun(ctx, helperCommand(t, "cat-file", "a.txt"))
	if err != nil {
		t.Fatalf("ExecRun in a preview with a shadow: %v", err)
	}
	// The helper is the test binary, which prints PASS after the file.
	if !strings.HasPrefix(resp.Stdout, "staged\n") {
		t.Fatalf("the command read %q, want the staged content", resp.Stdout)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(got) != "disk\n" {
		t.Fatalf("the workspace changed under a preview: %q", got)
	}
	// Once the edit is dropped, the command reads the disk again.
	r.ClearStaged()
	resp, err = r.ExecRun(ctx, helperCommand(t, "cat-file", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Stdout, "disk\n") {
		t.Fatalf("after ClearStaged the command read %q, want the disk content", resp.Stdout)
	}
}

// What a command writes in the shadow comes back to the turn as a staged
// edit, like an edit the model made — the disk untouched.
func TestShadow_ACommandsWriteIsStaged(t *testing.T) {
	r, root := newShadowRunner(t)
	ctx := context.Background()
	resp, err := r.ExecRun(ctx, helperCommand(t, "write-file", "b.txt"))
	if err != nil {
		t.Fatalf("ExecRun: %v", err)
	}
	if got := r.StagedFileContent(ctx)["b.txt"]; got != "from command\n" {
		t.Fatalf("the command's file is not staged: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err == nil {
		t.Fatal("the command's file reached the workspace during a preview")
	}
	if !strings.Contains(resp.Stderr, "b.txt") || !strings.Contains(resp.Stderr, "staged") {
		t.Errorf("the response does not say what was staged: %q", resp.Stderr)
	}
	// A second command sees it too, and a change on disk reaches the shadow.
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("disk again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err = r.ExecRun(ctx, helperCommand(t, "cat-file", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Stdout, "disk again\n") {
		t.Fatalf("a disk change did not reach the shadow: %q", resp.Stdout)
	}
	resp, err = r.ExecRun(ctx, helperCommand(t, "cat-file", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Stdout, "from command\n") {
		t.Fatalf("the staged file is gone from the shadow: %q", resp.Stdout)
	}
}

// Without the shadow a preview keeps refusing commands, as it always did;
// with it the refusal is gone, and the workspace's exclude_dirs are linked
// rather than copied.
func TestShadow_RefusalAndExcludes(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true, BlockExecInDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if !r.ExecRefusedByDryRun() {
		t.Fatal("a preview without a shadow must refuse commands")
	}
	if _, err := r.ExecRun(context.Background(), helperCommand(t, "cat-file", "a.txt")); err == nil {
		t.Fatal("a preview without a shadow ran a command")
	}

	r2, root2 := newShadowRunner(t)
	if r2.ExecRefusedByDryRun() {
		t.Fatal("a preview with a shadow refuses nothing")
	}
	if err := os.MkdirAll(filepath.Join(root2, "node_modules", "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root2, "node_modules", "dep", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh, err := r2.turn.shadow()
	if err != nil {
		t.Fatal(err)
	}
	if !sh.linked["node_modules"] {
		t.Skip("symlinks unavailable here; the excluded dir is left out")
	}
	if st, err := os.Lstat(filepath.Join(sh.Root(), "node_modules")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("node_modules is copied, not linked: %v %v", st, err)
	}
	if _, ok := sh.shadow["node_modules/dep/index.js"]; ok {
		t.Error("a file of an excluded dir was copied")
	}
}

// A workspace past the copy limit keeps refusing commands, with the reason.
func TestShadow_TooLargeRefuses(t *testing.T) {
	root := t.TempDir()
	prev := shadowMaxBytes
	shadowMaxBytes = 1024
	t.Cleanup(func() { shadowMaxBytes = prev })
	big := make([]byte, 600)
	for _, name := range []string{"x.bin", "y.bin"} {
		if err := os.WriteFile(filepath.Join(root, name), big, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewRunner(root, RunnerOptions{DryRun: true, BlockExecInDryRun: true, ShadowExec: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	_, err = r.ExecRun(context.Background(), helperCommand(t, "cat-file", "x.bin"))
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want the size refusal", err)
	}
}

// Every task layer has a shadow of its own (ORC-12): two workers of one turn
// run their commands against their own staged edits, not each other's.
func TestShadow_EachLayerHasItsOwn(t *testing.T) {
	r, _ := newShadowRunner(t)
	base := context.Background()
	a := WithLayer(base, r.ForkLayer(base))
	b := WithLayer(base, r.ForkLayer(base))
	hash := r.overlayAt(base).CurrentHash("a.txt")
	if _, err := r.FSWrite(a, FSWriteRequest{Path: "a.txt", Content: "from a\n", FileHash: hash}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.FSWrite(b, FSWriteRequest{Path: "a.txt", Content: "from b\n", FileHash: hash}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ctx  context.Context
		want string
	}{{a, "from a\n"}, {b, "from b\n"}, {base, "disk\n"}} {
		resp, err := r.ExecRun(tc.ctx, helperCommand(t, "cat-file", "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(resp.Stdout, tc.want) {
			t.Errorf("read %q, want %q", resp.Stdout, tc.want)
		}
	}
	// Two layers never share a shadow: their commands may run at once, and
	// one sync would undo the other's.
	shA, _ := r.shadowFor(r.overlayAt(a))
	shB, _ := r.shadowFor(r.overlayAt(b))
	if shA == nil || shA == shB {
		t.Fatal("the two layers share a shadow")
	}
	// Dropping a layer drops its shadow.
	layerB := r.overlayAt(b)
	r.DropLayer(base, layerB)
	r.shadowMu.Lock()
	_, still := r.shadows[layerB]
	r.shadowMu.Unlock()
	if still {
		t.Error("a dropped layer's shadow is still held")
	}
}

// VerificationRoot is the shadow, synced, in a preview; the workspace when
// the turn applies.
func TestVerificationRoot(t *testing.T) {
	r, root := newShadowRunner(t)
	ctx := context.Background()
	hash := r.overlayAt(ctx).CurrentHash("a.txt")
	if _, err := r.FSWrite(ctx, FSWriteRequest{Path: "a.txt", Content: "staged\n", FileHash: hash}); err != nil {
		t.Fatal(err)
	}
	got, inShadow, err := r.VerificationRoot(ctx)
	if err != nil || !inShadow || got == root {
		t.Fatalf("VerificationRoot = %q %v %v, want the shadow", got, inShadow, err)
	}
	if b, _ := os.ReadFile(filepath.Join(got, "a.txt")); string(b) != "staged\n" {
		t.Fatalf("the shadow holds %q, want the staged edit", b)
	}
	applying := r.NewTurn(TurnOptions{DryRun: false})
	defer applying.Close()
	got, inShadow, err = r.VerificationRoot(WithTurn(ctx, applying))
	if err != nil || inShadow || got != root {
		t.Fatalf("VerificationRoot for an applying turn = %q %v %v, want the workspace", got, inShadow, err)
	}
}

// A task layer's edits are on no disk until it commits, even in a turn that
// applies as it goes; its commands run in the layer's shadow, where the
// turn's own run on the real tree (ORC-8).
func TestShadow_ALayersCommandRunsInTheShadowWhenTheTurnApplies(t *testing.T) {
	r, root := newShadowRunner(t)
	base := context.Background()
	r.TurnAt(base).SetAllowExecDespiteDryRun(true)
	if r.TurnAt(base).CommandsInShadow() {
		t.Fatal("the turn's own commands run on the real tree once apply unlocks them")
	}
	layer := WithLayer(base, r.ForkLayer(base))
	hash := r.overlayAt(base).CurrentHash("a.txt")
	if _, err := r.FSWrite(layer, FSWriteRequest{Path: "a.txt", Content: "from the layer\n", FileHash: hash}); err != nil {
		t.Fatal(err)
	}
	resp, err := r.ExecRun(base, helperCommand(t, "cat-file", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Stdout, "disk\n") {
		t.Fatalf("the turn's command read %q, want the disk", resp.Stdout)
	}
	resp, err = r.ExecRun(layer, helperCommand(t, "cat-file", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Stdout, "from the layer\n") {
		t.Fatalf("the layer's command read %q, want its staged edit", resp.Stdout)
	}
	if _, err := r.ExecRun(layer, helperCommand(t, "write-file", "b.txt")); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.overlayAt(layer).EffectiveContent("b.txt"); !ok || got != "from command\n" {
		t.Fatalf("what the layer's command wrote is staged in the layer: %q %v", got, ok)
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("the layer's command wrote past the layer: %v", err)
	}
	if _, ok := r.overlayAt(base).EffectiveContent("b.txt"); ok {
		t.Fatal("the layer's write reached the turn before the layer committed")
	}
}
