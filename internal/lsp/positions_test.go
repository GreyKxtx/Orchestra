package lsp_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp"
)

func TestPathToURI_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only")
	}
	uri := lsp.PathToURI("/home/user/main.go")
	if uri != "file:///home/user/main.go" {
		t.Fatalf("got %q", uri)
	}
}

func TestPathToURI_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only")
	}
	uri := lsp.PathToURI(`C:\Users\user\main.go`)
	if !strings.HasPrefix(uri, "file:///C:/") {
		t.Fatalf("unexpected URI: %q", uri)
	}
}

func TestURIToPath_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only")
	}
	path, err := lsp.URIToPath("file:///home/user/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/home/user/main.go" {
		t.Fatalf("got %q", path)
	}
}

func TestURIToPath_InvalidScheme(t *testing.T) {
	_, err := lsp.URIToPath("http://example.com/foo")
	if err == nil {
		t.Fatal("expected error for non-file URI")
	}
}

func TestToolPosition_ToLSP_UTF8(t *testing.T) {
	tp := lsp.ToolPosition{Line: 5, Col: 10}
	pos := tp.ToLSP("utf-8", "some text here")
	if pos.Line != 4 {
		t.Errorf("line: got %d, want 4", pos.Line)
	}
	if pos.Character != 9 {
		t.Errorf("char: got %d, want 9", pos.Character)
	}
}

func TestToolPosition_ToLSP_ClampZero(t *testing.T) {
	tp := lsp.ToolPosition{Line: 0, Col: 0}
	pos := tp.ToLSP("utf-8", "")
	if pos.Line != 0 || pos.Character != 0 {
		t.Fatalf("unexpected pos: %+v", pos)
	}
}

func TestToolPositionFrom_UTF8(t *testing.T) {
	pos := lsp.Position{Line: 4, Character: 9}
	tp := lsp.ToolPositionFrom(pos, "utf-8", "some text here")
	if tp.Line != 5 {
		t.Errorf("line: got %d, want 5", tp.Line)
	}
	if tp.Col != 10 {
		t.Errorf("col: got %d, want 10", tp.Col)
	}
}

func TestToolPosition_UTF16_MultiByteChar(t *testing.T) {
	// "héllo": h=1B, é=2B in UTF-8 but 1 code unit in UTF-16.
	// Col=4 means byte offset 3 (after "hé", since é takes 2 bytes).
	tp := lsp.ToolPosition{Line: 1, Col: 4}
	pos := tp.ToLSP("utf-16", "héllo")
	// h(1 unit) + é(1 unit) = 2 units before byte offset 3.
	if pos.Character != 2 {
		t.Fatalf("utf-16 char: got %d, want 2", pos.Character)
	}
}

func TestToolPositionFrom_UTF16_MultiByteChar(t *testing.T) {
	// Reverse: UTF-16 offset 2 in "héllo" → byte offset 3 → Col=4.
	pos := lsp.Position{Line: 0, Character: 2}
	tp := lsp.ToolPositionFrom(pos, "utf-16", "héllo")
	if tp.Col != 4 {
		t.Fatalf("col: got %d, want 4", tp.Col)
	}
}

func TestToolPosition_UTF16_SurrogatePair(t *testing.T) {
	// U+1F600 (😀) is 4 bytes in UTF-8 and 2 code units in UTF-16.
	// "A😀B": A=1B, 😀=4B. Col=6 means byte offset 5 (after A + 😀).
	tp := lsp.ToolPosition{Line: 1, Col: 6}
	pos := tp.ToLSP("utf-16", "A\U0001F600B")
	// A(1) + 😀(2 surrogate) = 3 UTF-16 units before byte 5.
	if pos.Character != 3 {
		t.Fatalf("utf-16 char: got %d, want 3", pos.Character)
	}
}
