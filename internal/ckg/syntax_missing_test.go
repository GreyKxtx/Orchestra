package ckg

import (
	"strings"
	"testing"
)

// findSyntaxProblemNode looked for a node whose type is "MISSING". tree-sitter
// does not name nodes that way: a missing token is flagged on the node
// (IsMissing) and its type is the token that should have been there — "}" for
// an unclosed brace. So the whole MISSING branch was dead, and the gate passed
// every file whose only fault was something left out.
//
// An unclosed brace is the most ordinary way a generated edit breaks a file,
// and it went through.
func TestValidateSyntax_CatchesAMissingToken(t *testing.T) {
	broken := "package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n"
	err := ValidateSyntax("mathx.go", []byte(broken))
	if err == nil {
		t.Fatal("a Go file with an unclosed brace must be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "syntax") {
		t.Errorf("the error must say what is wrong, got: %v", err)
	}
}

func TestValidateSyntax_CatchesGarbage(t *testing.T) {
	if err := ValidateSyntax("x.go", []byte("}}} not code {{{\n")); err == nil {
		t.Error("an ERROR node must still be rejected")
	}
}

// The gate must not invent problems: these all parse.
func TestValidateSyntax_AcceptsWhatParses(t *testing.T) {
	cases := map[string]string{
		"plain function": "package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"generics":       "package main\n\nfunc Map[T any, U any](in []T, f func(T) U) []U {\n\tvar out []U\n\tfor _, v := range in {\n\t\tout = append(out, f(v))\n\t}\n\treturn out\n}\n",
		"comment only":   "// just a note\n",
		"empty":          "",
	}
	for name, src := range cases {
		if err := ValidateSyntax("x.go", []byte(src)); err != nil {
			t.Errorf("%s must parse: %v", name, err)
		}
	}
}

// A file whose extension has no grammar is none of the gate's business.
func TestValidateSyntax_SkipsWhatItCannotParse(t *testing.T) {
	if err := ValidateSyntax("notes.txt", []byte("}}} whatever {{{")); err != nil {
		t.Errorf("a text file must pass: %v", err)
	}
}
