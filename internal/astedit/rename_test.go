package astedit

import (
	"context"
	"strings"
	"testing"
)

func TestRenameInFile_Go_BasicIdentifier(t *testing.T) {
	src := []byte(`package main

func Foo() {}
func bar() { Foo() }
`)
	res, err := RenameInFile(context.Background(), "x.go", src, "Foo", "Baz")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 2 {
		t.Fatalf("want 2 hits, got %d", res.Count)
	}
	if !strings.Contains(string(res.NewContent), "func Baz()") {
		t.Errorf("def not renamed: %s", res.NewContent)
	}
	if !strings.Contains(string(res.NewContent), "bar() { Baz()") {
		t.Errorf("usage not renamed: %s", res.NewContent)
	}
}

func TestRenameInFile_Go_SkipsStringAndComment(t *testing.T) {
	src := []byte(`package main

// Foo is a function, not a comment about renaming.
func Foo() string { return "Foo lives in a string" }
`)
	res, err := RenameInFile(context.Background(), "x.go", src, "Foo", "Bar")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 1 {
		t.Fatalf("want exactly 1 hit (the def), got %d: %s", res.Count, res.NewContent)
	}
	if strings.Contains(string(res.NewContent), `"Bar lives`) {
		t.Errorf("string literal was rewritten: %s", res.NewContent)
	}
	if strings.Contains(string(res.NewContent), "// Bar is a function") {
		t.Errorf("comment was rewritten: %s", res.NewContent)
	}
}

func TestRenameInFile_DoesNotMatchSubstring(t *testing.T) {
	src := []byte(`package main
func Foo() {}
func FooBar() {}
`)
	res, err := RenameInFile(context.Background(), "x.go", src, "Foo", "X")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 1 {
		t.Fatalf("expected 1 hit (Foo only, NOT FooBar), got %d: %s", res.Count, res.NewContent)
	}
	if !strings.Contains(string(res.NewContent), "func FooBar()") {
		t.Errorf("FooBar should be untouched: %s", res.NewContent)
	}
}

func TestRenameInFile_NoChangeWhenOldEqualsNew(t *testing.T) {
	src := []byte(`package main
func A(){}
`)
	res, err := RenameInFile(context.Background(), "x.go", src, "A", "A")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 0 || string(res.NewContent) != string(src) {
		t.Fatalf("no-op rename should return src unchanged")
	}
}

func TestRenameInFile_UnsupportedExt(t *testing.T) {
	_, err := RenameInFile(context.Background(), "data.bin", []byte("x"), "x", "y")
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestRenameInFile_Python(t *testing.T) {
	src := []byte(`def foo():
    return foo

# foo in a comment
x = "foo in a string"
`)
	res, err := RenameInFile(context.Background(), "x.py", src, "foo", "bar")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 2 {
		t.Fatalf("want 2 hits, got %d: %s", res.Count, res.NewContent)
	}
	if strings.Contains(string(res.NewContent), `"bar in a string"`) {
		t.Errorf("string rewritten: %s", res.NewContent)
	}
	if strings.Contains(string(res.NewContent), "# bar in a comment") {
		t.Errorf("comment rewritten: %s", res.NewContent)
	}
}
