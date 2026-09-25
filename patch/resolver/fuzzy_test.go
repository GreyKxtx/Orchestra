package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/patch/patches"
)

func TestLevenshteinDistance(t *testing.T) {
	if d := levenshteinDistance("kitten", "sitting"); d != 3 {
		t.Fatalf("distance: got %d want 3", d)
	}
	if levenshteinDistance("", "") != 0 {
		t.Fatal("empty strings")
	}
}

func TestLineSimilarity(t *testing.T) {
	if lineSimilarity("hello", "hello") != 1.0 {
		t.Fatal("identical")
	}
	if lineSimilarity("return 1", "return 2") < 0.85 {
		t.Fatalf("similar lines should pass threshold, got %v", lineSimilarity("return 1", "return 2"))
	}
}

func TestFuzzyBlockFind_MiddleTypos(t *testing.T) {
	hay := "func f() {\n\treturn 1\n\tx := 2\n}\n"
	needle := "func f( ) {\n\treturn 1\n\tx := 2\n}\n"
	s, e, hits := fuzzyBlockFind(hay, needle)
	if hits != 1 {
		t.Fatalf("hits: want 1, got %d", hits)
	}
	if hay[s:e] != hay {
		t.Errorf("span %q", hay[s:e])
	}
}

func TestFuzzyBlockFind_TooDifferent(t *testing.T) {
	hay := "func f() {\n\treturn 1\n\tx := 2\n}\n"
	needle := "func helper() {\n\treturn 1\n\tx := 2\n}\n"
	_, _, hits := fuzzyBlockFind(hay, needle)
	if hits != 0 {
		t.Fatalf("hits: want 0, got %d", hits)
	}
}

func TestDoubleAnchorFind_Unit(t *testing.T) {
	hay := "package p\n\nfunc f() {\n\ta\n}\n\nfunc g() {}\n"
	needle := "package p\n\nfunc f() {\n\ta\n}\n"
	s, e, hits := doubleAnchorFind(hay, needle)
	if hits != 1 {
		t.Fatalf("hits: want 1, got %d", hits)
	}
	if hay[s:e] != "package p\n\nfunc f() {\n\ta\n}\n" {
		t.Errorf("got %q", hay[s:e])
	}
}

func TestForgivingFind_NinePassFuzzy(t *testing.T) {
	hay := "func f() {\n\treturn 1\n\tx := 2\n}\n"
	needle := "func f( ) {\n\treturn 1\n\tx := 2\n}\n"
	_, _, hits, strat := forgivingFind(hay, needle)
	if hits != 1 {
		t.Fatalf("hits: want 1, got %d", hits)
	}
	if strat != "fuzzy-block" {
		t.Fatalf("strategy: want fuzzy-block, got %q", strat)
	}
}

// LLM-12: the anchor passes checked the first and last lines only. A search
// block written against a body the model misremembered matched any body of
// the same length between two unique lines, and the whole function was
// replaced with text written for the wrong one — silently.
func TestResolveSearchReplace_AnchorsDoNotCoverADifferentBody(t *testing.T) {
	root := t.TempDir()
	original := "func sync() {\n\tdata := load()\n\tvalidate(data)\n\tsave(data)\n}\n"
	if err := os.WriteFile(filepath.Join(root, "s.go"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	misremembered := "func sync() {\n\tresp := fetch()\n\tcheck(resp)\n\tsend(resp)\n}\n"
	for _, search := range []string{
		misremembered,               // block-anchor: first and last line
		"// sync\n" + misremembered, // double-anchor needs two lines each side
	} {
		_, err := ResolveExternalPatches(root, []patches.Patch{{
			Type: patches.TypeFileSearchReplace, Path: "s.go",
			Search: search, Replace: "func sync() {\n\tresp := fetch2()\n}\n",
		}})
		if err == nil {
			t.Fatalf("a search block with a different body resolved:\n%s", search)
		}
	}
	// Drift the anchor passes exist for still resolves: indentation and a
	// misremembered token.
	drift := "func sync() {\n    data := load()\n    validate(data)\n    save(dat)\n}\n"
	if _, err := ResolveExternalPatches(root, []patches.Patch{{
		Type: patches.TypeFileSearchReplace, Path: "s.go", Search: drift, Replace: "func sync() {}\n",
	}}); err != nil {
		t.Fatalf("indent and one-token drift must still resolve: %v", err)
	}
}

// double-anchor: two lines each side match, the body between does not.
func TestDoubleAnchorFind_DifferentBody(t *testing.T) {
	hay := "package p\n\nfunc sync() {\n\tdata := load()\n\tvalidate(data)\n\tsave(data)\n\treturn\n}\n"
	needle := "\nfunc sync() {\n\tdata := load()\n\tcheck(resp)\n\tsend(resp)\n\treturn\n}\n"
	if _, _, hits := doubleAnchorFind(hay, needle); hits != 0 {
		t.Fatalf("a different body between matching anchors: %d hits", hits)
	}
	same := "\nfunc sync() {\n\tdata := load()\n\tvalidate(data)\n\tsave(data)\n\treturn\n}\n"
	// Blank lines shift the window, so the same body may match twice (an
	// ambiguity the resolver reports); it must match.
	if _, _, hits := doubleAnchorFind(hay, same); hits == 0 {
		t.Fatal("the same body did not match")
	}
}

func TestMiddleAgrees(t *testing.T) {
	win := []string{"func f() {", "\t\treturn 1", "", "\t\tx := 2", "}"}
	if !middleAgrees(win, []string{"func f() {", "    return 1", "    x := 2", "}"}) {
		t.Fatal("whitespace and blank lines are not a different body")
	}
	if middleAgrees(win, []string{"func f() {", "    panic(1)", "    y := 3", "}"}) {
		t.Fatal("a different body agreed")
	}
}
