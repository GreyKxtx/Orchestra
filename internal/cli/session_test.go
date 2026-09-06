package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
)

func writeTestConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := config.DefaultConfig(dir)
	if err := config.Save(filepath.Join(dir, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
}

// captureStdout is defined in memory_stats_test.go and reused here: it
// redirects os.Stdout for the duration of fn and returns everything written
// to it. The CLI handlers print with fmt.Println/Printf, so swapping
// os.Stdout is the only interception point available.

func TestSessionExportImportCLI(t *testing.T) {
	root := t.TempDir()
	writeTestConfig(t, root)
	origWD, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	id := sessionfile.NewID()
	snap := &sessionfile.Snapshot{
		ID:         id,
		Title:      "cli roundtrip",
		UIMessages: []sessionfile.UIMessage{{Role: "user", Text: "ping"}},
	}
	if err := sessionfile.Save(root, snap); err != nil {
		t.Fatal(err)
	}

	exportPath := filepath.Join(root, "out.session.json")
	sessionExportOut = exportPath
	if err := runSessionExport(nil, []string{id}); err != nil {
		t.Fatalf("export: %v", err)
	}

	importRoot := t.TempDir()
	writeTestConfig(t, importRoot)
	if err := os.Chdir(importRoot); err != nil {
		t.Fatal(err)
	}

	sessionImportForce = false
	if err := runSessionImport(nil, []string{exportPath}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if !sessionfile.SessionExists(importRoot, id) {
		t.Fatalf("session not imported")
	}
}

func TestSessionSearchAndForkCLI(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.WriteFile(filepath.Join(dir, ".orchestra.yml"),
		[]byte("project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := &sessionfile.Snapshot{
		Version: sessionfile.Version,
		ID:      "20260901T100000-aaaa",
		Title:   "wire the bearer token",
		UIMessages: []sessionfile.UIMessage{
			{Role: "user", Text: "wire the bearer token"},
			{Role: "assistant", Text: "authTransport sets the header"},
			{Role: "user", Text: "now do it differently"},
			{Role: "assistant", Text: "ok"},
		},
		// History as the product writes it: assistant and tool messages only,
		// no user turns. The two turns are located by recorded boundaries.
		History: []llm.Message{
			{Role: llm.RoleAssistant, Content: "calling grep"},
			{Role: llm.RoleTool, Content: "authTransport sets the header"},
			{Role: llm.RoleAssistant, Content: "ok"},
		},
		TurnStarts: []int{0, 2},
	}
	if err := sessionfile.Save(dir, snap); err != nil {
		t.Fatal(err)
	}

	sessionSearchInsensitive = false
	sessionSearchAll = false
	sessionSearchLimit = 0
	if err := runSessionSearch(nil, []string{"bearer"}); err != nil {
		t.Fatalf("session search: %v", err)
	}

	// A query that matches nothing is not a failure.
	if err := runSessionSearch(nil, []string{"zzz-no-such-text"}); err != nil {
		t.Fatalf("a query with no matches must exit cleanly: %v", err)
	}

	// Forking without --at must be refused, not silently default to some index.
	sessionForkAt = -1
	if err := runSessionFork(nil, []string{"20260901T100000-aaaa"}); err == nil {
		t.Fatal("fork without --at must be rejected")
	} else if !strings.Contains(err.Error(), "--at") {
		t.Errorf("error should mention --at, got: %v", err)
	}

	// Composition: the #<index> that 'session search' prints must be exactly
	// the index 'session fork --at' needs to branch at that same message.
	var searchErr error
	out := captureStdout(t, func() {
		searchErr = runSessionSearch(nil, []string{"differently"})
	})
	if searchErr != nil {
		t.Fatalf("session search: %v", searchErr)
	}
	// The blank line between sessions must not fire for the first one.
	if strings.HasPrefix(out, "\n") {
		t.Errorf("search output must not open on a blank line; got:\n%q", out)
	}
	idxRe := regexp.MustCompile(`#(\d+)\s+user\s+now do it differently`)
	m := idxRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("search output missing expected '#<index> user now do it differently' line; got:\n%s", out)
	}
	idx, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse index from search output: %v", err)
	}
	if idx != 2 {
		t.Fatalf("search printed index %d for %q, want 2 (the fixture's third message); output:\n%s", idx, "now do it differently", out)
	}

	sessionForkAt = idx
	if err := runSessionFork(nil, []string{"20260901T100000-aaaa"}); err != nil {
		t.Fatalf("session fork: %v", err)
	}

	metas, err := sessionfile.ListMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Fatalf("want the original plus one branch, got %d", len(metas))
	}
	var branchID string
	for _, m := range metas {
		if m.ID != "20260901T100000-aaaa" {
			branchID = m.ID
		}
	}
	branch, err := sessionfile.Load(dir, branchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch.UIMessages) != idx {
		t.Fatalf("branch UIMessages = %d, want %d (fork at the index 'session search' printed)", len(branch.UIMessages), idx)
	}
	if branch.ParentID != "20260901T100000-aaaa" {
		t.Errorf("ParentID = %q", branch.ParentID)
	}

	// Forking a non-user index must fail rather than produce a broken branch.
	sessionForkAt = 1
	if err := runSessionFork(nil, []string{"20260901T100000-aaaa"}); err == nil {
		t.Error("forking at an assistant message must be refused")
	}
}
