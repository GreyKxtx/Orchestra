package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAtRefPaths(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"look at @pkg/auth/token.go please", []string{"pkg/auth/token.go"}},
		// Attachments arrive as @refs too (attachments.MergeQueryWithFileRefs),
		// so one extractor covers both @-mentions and attached files.
		{"fix this\n\n@internal/mcp/remote.go @llm/probe.go",
			[]string{"internal/mcp/remote.go", "llm/probe.go"}},
		{"no refs here", nil},
		// An email or a decorator is not a file reference.
		{"mail me at user@example.com", nil},
		{"@", nil},
		// Trailing punctuation belongs to the sentence, not the path.
		{"see @pkg/auth/token.go.", []string{"pkg/auth/token.go"}},
		{"(@pkg/auth/token.go)", []string{"pkg/auth/token.go"}},
		// Absolute paths and traversal are not refs into this workspace.
		{"@/etc/passwd", nil},
		{"@../../secrets.txt", nil},
		// The same file twice is one directory to load.
		{"@a/b.go and @a/c.go", []string{"a/b.go", "a/c.go"}},
	}
	// Backslash handling follows the HOST, deliberately, and so does this
	// case. filepath.ToSlash rewrites separators on Windows and does nothing
	// on Linux — which is correct both times: the agent and the files it
	// resolves are on the same machine, a Windows user types backslashes and
	// means separators, and on Linux a backslash is a legal character in a
	// filename and means itself. Asserting one answer on both platforms is
	// what made CI red on Linux while passing here.
	if runtime.GOOS == "windows" {
		cases = append(cases, struct {
			in   string
			want []string
		}{`open @pkg\auth\token.go`, []string{"pkg/auth/token.go"}})
	} else {
		cases = append(cases, struct {
			in   string
			want []string
		}{`open @pkg\auth\token.go`, []string{`pkg\auth\token.go`}})
	}

	for _, c := range cases {
		got := atRefPaths(c.in)
		if len(got) != len(c.want) {
			t.Errorf("atRefPaths(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("atRefPaths(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

// The point of the feature: an @-mention must pull the rules of the file's
// directory into the prompt BEFORE the model reads the file, not after. Until
// now discoverInstructions only ran on fs.read, so a package's rules arrived a
// step late — or never, if the model answered from the mention alone.
func TestInstructionsForQuery_LoadsRulesOfMentionedDirs(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ORCHESTRA.md"),
		[]byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "token.go"), []byte("package auth"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := testMemoryRunner(t, root)
	got := r.InstructionsForQuery("what does @pkg/auth/token.go do?")

	if !strings.Contains(got, "AUTH PACKAGE RULES") {
		t.Fatalf("mentioned package's rules were not loaded: %q", got)
	}
	if !strings.Contains(got, "pkg/auth/ORCHESTRA.md") {
		t.Errorf("the text must name the file it came from: %q", got)
	}
}

// A directory already injected this turn must not be injected again when the
// model then reads the file — the seen-set is shared, so eager and lazy
// discovery cannot double up.
func TestInstructionsForQuery_SharesTheSeenSetWithFsRead(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ORCHESTRA.md"),
		[]byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := testMemoryRunner(t, root)
	if got := r.InstructionsForQuery("@pkg/auth/token.go"); !strings.Contains(got, "AUTH PACKAGE RULES") {
		t.Fatalf("first pass did not load the rules: %q", got)
	}
	if got := r.discoverInstructions(sub); got != "" {
		t.Errorf("the same directory was offered twice in one turn: %q", got)
	}
}

// A query with no refs must cost nothing and add nothing.
func TestInstructionsForQuery_EmptyWithoutRefs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ORCHESTRA.md"), []byte("ROOT RULES"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := testMemoryRunner(t, root)
	if got := r.InstructionsForQuery("just a question"); got != "" {
		t.Errorf("a query with no refs produced %q", got)
	}
}

// A ref pointing outside the workspace must not read anything: the walk is
// bounded by the root, and "@../../.." is not a mention of this project.
func TestInstructionsForQuery_IgnoresRefsOutsideTheWorkspace(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "ORCHESTRA.md"),
		[]byte("SECRET RULES"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(outside, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	r := testMemoryRunner(t, root)
	if got := r.InstructionsForQuery("look at @../ORCHESTRA.md"); strings.Contains(got, "SECRET RULES") {
		t.Fatalf("a ref outside the workspace was followed: %q", got)
	}
}

// Users mention directories, not only files: "look at @pkg/auth". The ref
// resolver took the parent of whatever was named, so mentioning a package
// loaded its PARENT's rules and skipped its own — the one file the mention was
// actually about.
func TestInstructionsForQuery_DirectoryMentionLoadsThatDirectory(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ORCHESTRA.md"),
		[]byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"look at @pkg/auth", "look at @pkg/auth/"} {
		r := testMemoryRunner(t, root)
		if got := r.InstructionsForQuery(q); !strings.Contains(got, "AUTH PACKAGE RULES") {
			t.Errorf("%q did not load the mentioned directory's own rules: %q", q, got)
		}
	}
}
