package instrument

import (
	"os"
	"path/filepath"
)

// Detect scans dir for known manifest files and returns matching LangConfigs from candidates.
// If multiple languages match, all are returned (e.g. a TS project also has package.json).
// The slice is de-duplicated: if both tsconfig.json and package.json are present, TypeScript
// takes precedence and JavaScript is dropped, since tsLang is listed first in Phase1Langs.
func Detect(dir string, candidates []LangConfig) []LangConfig {
	var found []LangConfig
	seen := map[string]bool{}

	for _, lc := range candidates {
		if seen[lc.Name] {
			continue
		}
		for _, f := range lc.DetectFiles {
			if fileExists(filepath.Join(dir, f)) {
				found = append(found, lc)
				seen[lc.Name] = true
				break
			}
		}
	}

	// If TypeScript is detected, skip JavaScript (same runtime, ts wins).
	if seen["typescript"] && seen["javascript"] {
		filtered := found[:0]
		for _, lc := range found {
			if lc.Name != "javascript" {
				filtered = append(filtered, lc)
			}
		}
		found = filtered
	}

	return found
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
