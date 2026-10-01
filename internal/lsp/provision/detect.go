package provision

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp/registry"
)

// Detect finds language servers likely needed for workspaceRoot by root markers
// and a shallow scan for source extensions. Order follows registry.All().
func Detect(workspaceRoot string) []registry.Entry {
	root := filepath.Clean(workspaceRoot)
	if root == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []registry.Entry

	add := func(e registry.Entry) {
		if seen[e.ID] {
			return
		}
		seen[e.ID] = true
		out = append(out, e)
	}

	for _, e := range registry.All() {
		if markersHit(root, e.RootMarkers) {
			add(e)
		}
	}

	// Shallow walk: root + one level of dirs (skip heavy trees).
	extHit := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "vendor" ||
				base == "dist" || base == "build" || base == ".orchestra" ||
				base == "bin" || base == "obj" || base == ".idea" || base == ".vs" {
				return filepath.SkipDir
			}
			// Depth limit: root=0, skip deeper than 2.
			if rel != "." && strings.Count(rel, string(os.PathSeparator)) >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if orchestraOwnFile(d.Name()) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == "" || extHit[ext] {
			return nil
		}
		if e, ok := registry.ByExtension(ext); ok {
			extHit[ext] = true
			add(e)
		}
		return nil
	})

	return out
}

// orchestraOwnFile reports Orchestra's own workspace files. Every workspace
// has .orchestra.yml, so counting it as project YAML asked for a YAML language
// server in every project.
func orchestraOwnFile(name string) bool {
	switch strings.ToLower(name) {
	case ".orchestra.yml", ".orchestra.yaml", ".orchestra.env":
		return true
	}
	return false
}

func markersHit(root string, markers []string) bool {
	for _, m := range markers {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if strings.ContainsAny(m, "*?[") {
			matches, err := filepath.Glob(filepath.Join(root, m))
			if err == nil && len(matches) > 0 {
				return true
			}
			continue
		}
		if fileExists(filepath.Join(root, m)) {
			return true
		}
	}
	return false
}
