package fs

import "testing"

func TestMatchGlobPath_Star(t *testing.T) {
	if !matchGlobPath("*.go", "main.go") {
		t.Error("*.go should match main.go")
	}
	if matchGlobPath("*.go", "sub/main.go") {
		t.Error("*.go should not match sub/main.go")
	}
}

func TestMatchGlobPath_DoubleStar(t *testing.T) {
	if !matchGlobPath("**/*.go", "main.go") {
		t.Error("**/*.go should match main.go (zero segments)")
	}
	if !matchGlobPath("**/*.go", "a/b/c.go") {
		t.Error("**/*.go should match a/b/c.go")
	}
	if matchGlobPath("**/*.go", "a/b/c.txt") {
		t.Error("**/*.go should not match a/b/c.txt")
	}
}

func TestMatchGlobPath_DoubleStarAlone(t *testing.T) {
	if !matchGlobPath("**", "anything") {
		t.Error("** should match anything")
	}
	if !matchGlobPath("**", "a/b/c") {
		t.Error("** should match a/b/c")
	}
}
