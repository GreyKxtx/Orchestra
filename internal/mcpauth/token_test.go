package mcpauth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isolateHome points os.UserHomeDir at a temp dir on both Windows and Unix
// so a developer's real ~/.orchestra/mcp-oauth cannot influence assertions.
// Mirrors internal/config/global_test.go's isolateHome.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestSaveLoadToken_RoundTrips(t *testing.T) {
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
	if err := SaveToken("linear", want); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := LoadToken("linear")
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadToken_MissingReturnsErrNoToken(t *testing.T) {
	isolateHome(t)
	if _, err := LoadToken("never-configured"); !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

func TestDeleteToken_IsIdempotent(t *testing.T) {
	home := isolateHome(t)
	if err := SaveToken("linear", Token{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".orchestra", "mcp-oauth", "linear.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("token file must exist after SaveToken: %v", err)
	}

	if err := DeleteToken("linear"); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("token file must be gone after DeleteToken")
	}
	if err := DeleteToken("linear"); err != nil {
		t.Fatalf("second delete must be a no-op, got %v", err)
	}
}

func TestTokenPath_RejectsPathTraversalAndEmptyNames(t *testing.T) {
	isolateHome(t)
	for _, name := range []string{"", ".", "..", "../evil", "a/b", "a\\b", "/etc/passwd"} {
		if _, err := tokenPath(name); err == nil {
			t.Errorf("tokenPath(%q) must be rejected, got no error", name)
		}
	}
}

func TestTokenPath_PlacesFileUnderGlobalMCPOAuthDir(t *testing.T) {
	home := isolateHome(t)
	path, err := tokenPath("linear")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".orchestra", "mcp-oauth", "linear.json")
	if path != want {
		t.Fatalf("tokenPath = %q, want %q", path, want)
	}
}
