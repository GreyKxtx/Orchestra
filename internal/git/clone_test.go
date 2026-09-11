package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCloneURL(t *testing.T) {
	ok := []struct {
		in   string
		name string
	}{
		{"https://github.com/owner/repo.git", "repo"},
		{"https://github.com/owner/repo", "repo"},
		{"https://github.com/owner/repo/", "repo"},
		{"http://127.0.0.1:8080/mirror/thing.git", "thing"},
		{"ssh://git@github.com/owner/repo.git", "repo"},
		{"git@github.com:owner/repo.git", "repo"},
		{"git@github.com:owner/sub/deep-repo.git", "deep-repo"},
	}
	for _, c := range ok {
		got, err := ParseCloneURL(c.in)
		if err != nil {
			t.Fatalf("ParseCloneURL(%q) = error %v, want name %q", c.in, err, c.name)
		}
		if got.Name != c.name {
			t.Errorf("ParseCloneURL(%q).Name = %q, want %q", c.in, got.Name, c.name)
		}
		if got.Raw != c.in {
			t.Errorf("ParseCloneURL(%q).Raw = %q, want it unchanged", c.in, got.Raw)
		}
	}

	// Every one of these would otherwise reach git's command line.
	bad := []string{
		"",
		"   ",
		"--upload-pack=calc.exe",
		"-u",
		"file:///c:/secrets",
		"/etc/passwd",
		"C:\\Users\\me\\repo",
		"https://github.com/",
		"https:///owner/repo.git",
		"https://github.com/owner/..",
		"https://github.com/owner/.git",
		"git@github.com:owner/repo.git\nrm -rf /",
	}
	for _, in := range bad {
		if got, err := ParseCloneURL(in); err == nil {
			t.Errorf("ParseCloneURL(%q) = %+v, want an error", in, got)
		}
	}
}

func TestCloneRefusesExistingDestination(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Clone(context.Background(), "https://github.com/owner/repo.git", parent)
	if err == nil {
		t.Fatal("Clone into an existing directory returned no error")
	}
	// It must fail before running git at all — no network in unit tests.
	if got := err.Error(); !contains(got, "already exists") {
		t.Fatalf("Clone error = %q, want it to name the existing directory", got)
	}
}

func TestCloneRefusesMissingParent(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := Clone(context.Background(), "https://github.com/owner/repo.git", missing); err == nil {
		t.Fatal("Clone into a missing parent returned no error")
	}
}

func TestCloneRejectsBadURLBeforeTouchingDisk(t *testing.T) {
	parent := t.TempDir()
	if _, err := Clone(context.Background(), "--upload-pack=calc", parent); err == nil {
		t.Fatal("Clone accepted an option-shaped URL")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Clone left %d entries behind after refusing", len(entries))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
