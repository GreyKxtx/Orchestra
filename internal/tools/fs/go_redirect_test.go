package fs

import (
	"strings"
	"testing"
)

// read answers a .go file with its symbol list instead of its text. That is
// worth doing for a long file and actively harmful for a short one, and it
// used to happen for every .go file the index knew about.
//
// What it cost is specific. The redirect REPLACES the content, so a file
// withheld this way cannot be read at all — and the package clause is the one
// line the symbol list does not carry, because the index stores symbols and
// every other tool names a Go symbol by its import path. For `package main`
// at a module root those differ. In an evaluation run the model was told
// three times that Item lived in `evalws`, never saw `package main`, wrote
// the file back with `package evalws`, and broke the build. It had gotten
// everything else about the task right.

func TestGoRedirect_ShortFileIsNotWithheld(t *testing.T) {
	// The real fixture: 91 bytes, told "the file may be thousands of lines".
	const short = "package main\n\n// Item is one line of an order.\ntype Item struct {\n\tName string\n\tQty  int\n}\n"
	if goFileIsLongEnoughToRedirect(short) {
		t.Error("a 7-line file must be answered with its text, not with a symbol list")
	}
}

func TestGoRedirect_LongFileStillIs(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n")
	for i := 0; i < goRedirectMinLines; i++ {
		b.WriteString("\n")
	}
	if !goFileIsLongEnoughToRedirect(b.String()) {
		t.Error("a long file is exactly what the redirect is for")
	}
}

func TestGoPackageClause(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"plain", "package main\n\nfunc main() {}\n", "package main"},
		{"after a line comment", "// Package foo does things.\npackage foo\n", "package foo"},
		{"after a build tag", "//go:build linux\n\npackage sys\n", "package sys"},
		{"after a block comment", "/*\nCopyright.\n*/\npackage law\n", "package law"},
		{"block comment on one line", "/* c */ package inline\n", "package inline"},
		{"leading blank lines", "\n\n\npackage late\n", "package late"},
		{"no clause at all", "func Add(a, b int) int { return a + b }\n", ""},
		{"empty file", "", ""},
		{"only comments", "// nothing here\n", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := goPackageClause(tt.src); got != tt.want {
				t.Errorf("goPackageClause() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A file long enough to be withheld must still hand over its package clause,
// or the redirect keeps the one line a whole-file write cannot do without.
func TestFormatGoFileRedirect_CarriesTheSymbols(t *testing.T) {
	out := FormatGoFileRedirect("item.go", "sha256:abc", []GoSymbol{
		{ShortName: "Item", Kind: "struct", LineStart: 4, LineEnd: 7},
	})
	if out == "" {
		t.Fatal("a file with symbols must produce a redirect")
	}
	for _, want := range []string{"item.go", "Item", "sha256:abc", "max_bytes"} {
		if !strings.Contains(out, want) {
			t.Errorf("the redirect must mention %q:\n%s", want, out)
		}
	}
	// The old text claimed every redirected file "may be thousands of lines",
	// which was the excuse for withholding a 7-line one.
	if strings.Contains(out, "wasteful") {
		t.Error("the redirect should say what it is doing, not scold the caller")
	}
}
