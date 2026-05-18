package repomap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuild_GoFile_ExtractsTopLevel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", `package main

import "fmt"

type User struct{ Name string }

func main() { fmt.Println("hi") }

func (u User) Greet() string { return u.Name }

func internal() {}
`)
	rm, err := Build(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(rm.Files))
	}
	got := rm.Files[0]
	if got.Lang != "go" {
		t.Errorf("lang = %q want go", got.Lang)
	}
	have := func(name string) bool {
		for _, s := range got.Symbols {
			if s.Name == name || s.Name == "User."+name {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"User", "main", "Greet", "internal"} {
		if !have(want) {
			t.Errorf("missing symbol %q (got: %+v)", want, got.Symbols)
		}
	}
}

func TestBuild_SkipsExcludedDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "node_modules/junk.js", `function ignored(){}`)
	writeFile(t, root, "src/keep.go", `package main
func keeper() {}`)
	rm, err := Build(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rm.Files {
		if strings.Contains(f.Path, "node_modules") {
			t.Errorf("should have skipped node_modules: %s", f.Path)
		}
	}
}

func TestBuild_RespectsMaxFiles(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 10; i++ {
		writeFile(t, root, "src/"+string(rune('a'+i))+".go",
			"package main\nfunc f() {}")
	}
	rm, err := Build(context.Background(), root, Options{MaxFiles: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Files) > 3 {
		t.Errorf("max_files=3 not honoured: got %d", len(rm.Files))
	}
}

func TestFormat_FullFitsUntouched(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", `package x
func A() {}
`)
	rm, _ := Build(context.Background(), root, Options{})
	out := Format(rm, 0)
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "func A") {
		t.Fatalf("missing content: %q", out)
	}
}

func TestFormat_Budget_DropsPrivateThenFiles(t *testing.T) {
	root := t.TempDir()
	// Big file with many private + public symbols.
	var b strings.Builder
	b.WriteString("package big\n")
	for i := 0; i < 10; i++ {
		b.WriteString("func Public" + string(rune('A'+i)) + "() {}\n")
		b.WriteString("func private" + string(rune('a'+i)) + "() {}\n")
	}
	writeFile(t, root, "big.go", b.String())
	writeFile(t, root, "small.go", `package x
func S() {}
`)

	rm, _ := Build(context.Background(), root, Options{})
	full := Format(rm, 0)

	// Render the public-only variant to know the exact size of layer-2 output,
	// then choose a budget that's between layer-2 and full so we exercise the
	// "prune private but keep all files" branch deterministically.
	publicOnly := make([]FileOutline, 0, len(rm.Files))
	for _, f := range rm.Files {
		if len(f.Symbols) <= 6 {
			publicOnly = append(publicOnly, f)
			continue
		}
		kept := make([]Symbol, 0, len(f.Symbols))
		for _, s := range f.Symbols {
			if !s.Private {
				kept = append(kept, s)
			}
		}
		publicOnly = append(publicOnly, FileOutline{Path: f.Path, Symbols: kept})
	}
	layer2Size := len(renderFiles(publicOnly))
	// Budget halfway between layer2 and full so layer 2 fits and we keep all files.
	mid := Format(rm, (layer2Size+len(full))/2)
	if strings.Contains(mid, "func privateA") {
		t.Errorf("private symbol survived budget pruning:\n%s", mid)
	}
	if !strings.Contains(mid, "small.go") {
		t.Errorf("small file should be kept when budget allows pruning (budget=%d layer2=%d full=%d): %s",
			(layer2Size+len(full))/2, layer2Size, len(full), mid)
	}

	// Tiny budget — should drop files entirely or fall back to sentinel.
	tiny := Format(rm, 30)
	if !strings.Contains(tiny, "omitted") && !strings.Contains(tiny, "too small") {
		t.Errorf("expected omission/sentinel for tiny budget, got:\n%s", tiny)
	}
}
