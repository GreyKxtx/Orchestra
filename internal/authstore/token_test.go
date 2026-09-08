package authstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isolateHome points os.UserHomeDir at a temp dir on both Windows and Unix
// so a developer's real ~/.orchestra cannot influence assertions.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeRaw(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}

func TestSaveLoad_RoundTrips(t *testing.T) {
	isolateHome(t)
	want := Token{
		TokenURL:     "https://auth.example.com/token",
		ClientID:     "client-abc",
		ClientSecret: "shh",
		AccessToken:  "at-123",
		TokenType:    "Bearer",
		RefreshToken: "rt-456",
		Expiry:       time.Now().Add(time.Hour).UTC().Round(time.Second),
	}
	if err := Save("llm-oauth", "corp", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load("llm-oauth", "corp")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoad_MissingReturnsErrNoToken(t *testing.T) {
	isolateHome(t)
	if _, err := Load("llm-oauth", "never-configured"); !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

func TestDelete_IsIdempotent(t *testing.T) {
	home := isolateHome(t)
	if err := Save("llm-oauth", "corp", Token{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".orchestra", "llm-oauth", "corp.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("token file must exist after Save: %v", err)
	}
	if err := Delete("llm-oauth", "corp"); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("token file must be gone after Delete")
	}
	if err := Delete("llm-oauth", "corp"); err != nil {
		t.Fatalf("second delete must be a no-op, got %v", err)
	}
}

func TestPath_RejectsPathTraversalAndEmptyNames(t *testing.T) {
	isolateHome(t)
	for _, name := range []string{"", ".", "..", "../evil", "a/b", "a\\b", "/etc/passwd"} {
		if _, err := Path("llm-oauth", name); err == nil {
			t.Errorf("Path(%q) must be rejected, got no error", name)
		}
	}
}

// Namespaces are as attacker-controllable as names once a second caller
// exists, so they get the same guard.
func TestPath_RejectsTraversalInNamespace(t *testing.T) {
	isolateHome(t)
	for _, ns := range []string{"", ".", "..", "../evil", "a/b", "a\\b"} {
		if _, err := Path(ns, "corp"); err == nil {
			t.Errorf("Path(namespace=%q) must be rejected, got no error", ns)
		}
	}
}

func TestPath_PlacesFileUnderNamespaceDir(t *testing.T) {
	home := isolateHome(t)
	path, err := Path("llm-oauth", "corp")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".orchestra", "llm-oauth", "corp.json")
	if path != want {
		t.Fatalf("Path = %q, want %q", path, want)
	}
}
