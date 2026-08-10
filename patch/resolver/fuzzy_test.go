package resolver

import "testing"

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
