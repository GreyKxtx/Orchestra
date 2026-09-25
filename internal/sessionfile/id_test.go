package sessionfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidID(t *testing.T) {
	for _, ok := range []string{NewID(), "20260805T150405-7f3a", "my_session.2", "a"} {
		if !ValidID(ok) {
			t.Errorf("%q must be valid", ok)
		}
	}
	for _, bad := range []string{"", "..", ".", "../../x/package", "a/b", `a\b`, "-x", ".hidden", "a b", strings.Repeat("a", 129)} {
		if ValidID(bad) {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// A client-chosen id never reaches the filesystem outside .orchestra/sessions.
func TestStoreRefusesIDsThatLeaveTheSessionsDir(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "x", "package.json")
	if err := os.MkdirAll(filepath.Dir(victim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, "../../x/package"); err == nil {
		t.Fatal("Delete must refuse a traversing id")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the file outside the sessions dir must survive: %v", err)
	}
	if _, err := Load(root, "../../x/package"); err == nil {
		t.Fatal("Load must refuse a traversing id")
	}
	if err := Save(root, &Snapshot{ID: "../evil"}); err == nil {
		t.Fatal("Save must refuse a traversing id")
	}
}
