package importrules

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A mode with no code of its own is its roles.Spec and its prompt file, and
// nothing else. scout and product are two such modes: their names appear in
// no other non-test Go file, so their tool lists, write scopes, the task
// tool's subagent_type enum, the <available_agents> cards, the default agency
// flows, the phase gate and config's reserved names all come from the Spec.
// Adding a mode used to mean editing 6–7 files that had to agree.
func TestASpecOnlyModeLivesInTwoFiles(t *testing.T) {
	root := repoRoot(t)
	for _, mode := range []string{"scout", "product"} {
		literal := `"` + mode + `"`
		var found []string
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "vendor", "testdata", "dist", "out":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), literal) {
				rel, _ := filepath.Rel(root, path)
				found = append(found, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0] != "internal/roles/roles.go" {
			t.Errorf("mode %s is named in %v; it should need only internal/roles/roles.go and its prompt file", mode, found)
		}
		if _, err := os.Stat(filepath.Join(root, "internal", "prompt", "files", mode+".txt")); err != nil {
			t.Errorf("mode %s has no prompt file: %v", mode, err)
		}
	}
}
