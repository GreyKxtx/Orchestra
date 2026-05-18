package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRef(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpandRefs_BasicSubstitution(t *testing.T) {
	refs := map[string]string{"hello": "WORLD"}
	got, err := ExpandRefs("prefix @refs/hello suffix", refs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "prefix WORLD suffix" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_AtStartOfLine(t *testing.T) {
	refs := map[string]string{"foo": "BAR"}
	got, err := ExpandRefs("@refs/foo\nnext line", refs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "BAR\n") {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_DoesNotMatchEmailLike(t *testing.T) {
	// `user@refs/foo` should NOT be treated as a macro (must be at start
	// or after whitespace).
	refs := map[string]string{"foo": "EXPANDED"}
	got, err := ExpandRefs("user@refs/foo", refs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "user@refs/foo" {
		t.Fatalf("got %q (should not have expanded)", got)
	}
}

func TestExpandRefs_UnknownRefIsError(t *testing.T) {
	_, err := ExpandRefs("text @refs/missing here", map[string]string{})
	if err == nil {
		t.Fatal("expected error for unknown ref")
	}
	if !strings.Contains(err.Error(), "unknown reference") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestExpandRefs_RecursiveExpansion(t *testing.T) {
	refs := map[string]string{
		"outer": "before @refs/inner after",
		"inner": "INNER",
	}
	got, err := ExpandRefs("[ @refs/outer ]", refs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "[ before INNER after ]" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_CycleDetected(t *testing.T) {
	refs := map[string]string{
		"a": "@refs/b",
		"b": "@refs/a",
	}
	_, err := ExpandRefs("@refs/a", refs)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	// May be reported either as cycle or as max-depth, depending on which
	// the recursion hits first. Both are acceptable signals.
	if !strings.Contains(err.Error(), "cycle") && !strings.Contains(err.Error(), "max expansion depth") {
		t.Fatalf("expected cycle/depth error, got: %v", err)
	}
}

func TestPrepareBody_RefsThenArguments(t *testing.T) {
	// Refs expansion happens before $ARGUMENTS substitution, so a ref
	// containing $ARGUMENTS still gets substituted.
	refs := map[string]string{"greeting": "Hello, $ARGUMENTS!"}
	got, err := PrepareBody("Body says: @refs/greeting", "world", refs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Body says: Hello, world!" {
		t.Fatalf("got %q", got)
	}
}

func TestPrepareBody_NilRefsRejectsMacro(t *testing.T) {
	_, err := PrepareBody("uses @refs/missing", "x", nil)
	if err == nil {
		t.Fatal("expected error: nil refs map with present macro")
	}
}

func TestPrepareBody_NoMacros_NilRefsOK(t *testing.T) {
	got, err := PrepareBody("plain body with $ARGUMENTS", "X", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain body with X" {
		t.Fatalf("got %q", got)
	}
}

func TestDiscoverRefsFromAll_ProjectOverridesUserOverridesPack(t *testing.T) {
	packsRoot := t.TempDir()
	writeRef(t, filepath.Join(packsRoot, "pkg1", "refs"), "a.md", "pack-a")
	writeRef(t, filepath.Join(packsRoot, "pkg1", "refs"), "b.md", "pack-b")

	userDir := t.TempDir()
	writeRef(t, userDir, "a.md", "user-a")
	writeRef(t, userDir, "c.md", "user-c")

	projDir := t.TempDir()
	writeRef(t, projDir, "a.md", "proj-a")

	refs, err := DiscoverRefsFromAll(packsRoot, userDir, projDir)
	if err != nil {
		t.Fatal(err)
	}
	if refs["a"] != "proj-a" {
		t.Errorf("project override failed: %q", refs["a"])
	}
	if refs["b"] != "pack-b" {
		t.Errorf("pack ref missing: %q", refs["b"])
	}
	if refs["c"] != "user-c" {
		t.Errorf("user-only ref missing: %q", refs["c"])
	}
	if len(refs) != 3 {
		t.Errorf("got %d refs, want 3: %+v", len(refs), refs)
	}
}

func TestDiscoverRefsFromAll_MissingDirsOK(t *testing.T) {
	refs, err := DiscoverRefsFromAll("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected empty, got %+v", refs)
	}
}
