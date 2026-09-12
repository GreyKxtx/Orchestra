package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func sameRootForTest(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func writeRootFixture(t *testing.T, root, projectRoot string) string {
	t.Helper()
	path := filepath.Join(root, ".orchestra.yml")
	body := "project_root: " + projectRoot + "\nllm:\n  api_base: http://127.0.0.1:1/v1\n  model: m\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// project_root is written relative to the file it is in — `orchestra init`
// writes "." — and it used to be taken as relative to the process's working
// directory instead. A core started by `orchestra web` or the desktop shell
// runs wherever that process happened to start, so the tools' root, the CKG
// database and the project id all landed in that directory, not in the
// project the file belongs to.
func TestLoad_ResolvesRelativeProjectRootAgainstTheConfigFile(t *testing.T) {
	root := t.TempDir()
	path := writeRootFixture(t, root, ".")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.ProjectRoot) {
		t.Fatalf("project_root %q must come back absolute", cfg.ProjectRoot)
	}
	if !sameRootForTest(cfg.ProjectRoot, root) {
		t.Fatalf("project_root = %q, want the config file's own directory %q", cfg.ProjectRoot, root)
	}
}

func TestLoad_ResolvesANestedRelativeProjectRoot(t *testing.T) {
	root := t.TempDir()
	path := writeRootFixture(t, root, "./src")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "src"); !sameRootForTest(cfg.ProjectRoot, want) {
		t.Fatalf("project_root = %q, want %q", cfg.ProjectRoot, want)
	}
}

func TestLoad_KeepsAnAbsoluteProjectRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	path := writeRootFixture(t, root, filepath.ToSlash(other))

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sameRootForTest(cfg.ProjectRoot, other) {
		t.Fatalf("project_root = %q, want the absolute path as written %q", cfg.ProjectRoot, other)
	}
}

// The committed config says "." on purpose: it is the same file on every
// machine. A settings save must write it back as written, not as the
// absolute path Load resolved it to — that would put one machine's path into
// a shared file on every round trip through the settings panel.
func TestSave_KeepsTheProjectRootAsWritten(t *testing.T) {
	root := t.TempDir()
	path := writeRootFixture(t, root, ".")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LLM.Model = "changed"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "project_root: .\n") {
		t.Fatalf("project_root must be saved as written; file:\n%s", data)
	}
	if strings.Contains(string(data), filepath.ToSlash(root)) || strings.Contains(string(data), root) {
		t.Fatalf("the absolute root leaked into the file:\n%s", data)
	}

	// And it still resolves the same way afterwards.
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sameRootForTest(again.ProjectRoot, root) || again.LLM.Model != "changed" {
		t.Fatalf("reload: root=%q model=%q", again.ProjectRoot, again.LLM.Model)
	}
}

// A root changed in memory — the CLI's --worktree does this — is a real
// change and is saved as such.
func TestSave_WritesAChangedProjectRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	path := writeRootFixture(t, root, ".")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ProjectRoot = other
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sameRootForTest(again.ProjectRoot, other) {
		t.Fatalf("project_root = %q, want the new root %q", again.ProjectRoot, other)
	}
}
