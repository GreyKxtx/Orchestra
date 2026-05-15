package instrument

import (
	"os"
	"path/filepath"
	"testing"
)

// --- shared test helpers (used by both test files) ---

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func langNames(lcs []LangConfig) []string {
	names := make([]string, len(lcs))
	for i, lc := range lcs {
		names[i] = lc.Name
	}
	return names
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// --- Detect() tests ---

func TestDetect_Empty(t *testing.T) {
	dir := t.TempDir()
	got := Detect(dir, AllLangs)
	if len(got) != 0 {
		t.Fatalf("expected no detection in empty dir, got %v", langNames(got))
	}
}

func TestDetect_Go(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21\n"), 0644))
	got := Detect(dir, []LangConfig{goLang})
	if len(got) != 1 || got[0].Name != "go" {
		t.Fatalf("expected [go], got %v", langNames(got))
	}
}

func TestDetect_Python(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644))
	got := Detect(dir, []LangConfig{pythonLang})
	if len(got) != 1 || got[0].Name != "python" {
		t.Fatalf("expected [python], got %v", langNames(got))
	}
}

func TestDetect_CSharpGlob(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "MyApp.csproj"), []byte("<Project/>"), 0644))
	got := Detect(dir, []LangConfig{csharpLang})
	if len(got) != 1 || got[0].Name != "csharp" {
		t.Fatalf("expected [csharp] via glob *.csproj, got %v", langNames(got))
	}
}

func TestDetect_TypeScriptBeatsJS(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{}"), 0644))
	must(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644))
	got := Detect(dir, AllLangs)
	names := langNames(got)
	for _, n := range names {
		if n == "javascript" {
			t.Errorf("javascript should be suppressed when typescript detected; got %v", names)
		}
	}
	if !containsStr(names, "typescript") {
		t.Errorf("expected typescript in result, got %v", names)
	}
	if len(got) != 1 {
		t.Errorf("expected exactly 1 result (typescript only), got %v", names)
	}
}

func TestDetect_GoAndPython(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21\n"), 0644))
	must(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644))
	got := Detect(dir, AllLangs)
	names := langNames(got)
	if !containsStr(names, "go") || !containsStr(names, "python") {
		t.Fatalf("expected both go and python detected, got %v", names)
	}
	if len(got) != 2 {
		t.Errorf("expected exactly 2 results (go + python), got %v", names)
	}
}
