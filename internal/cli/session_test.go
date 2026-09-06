package cli

import (
	"os"
	"path/filepath"
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
		History: []llm.Message{
			{Role: llm.RoleUser, Content: "wire the bearer token"},
			{Role: llm.RoleAssistant, Content: "authTransport sets the header"},
			{Role: llm.RoleUser, Content: "now do it differently"},
			{Role: llm.RoleAssistant, Content: "ok"},
		},
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

	sessionForkAt = 2
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
	if len(branch.UIMessages) != 2 {
		t.Fatalf("branch UIMessages = %d, want 2", len(branch.UIMessages))
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
