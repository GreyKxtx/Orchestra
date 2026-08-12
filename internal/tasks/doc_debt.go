package tasks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/orchestra/orchestra/internal/orchestrastate"
)

// Doc debt wiring (spec §2.3.2, checklist 17): the Docs Lead's MANIFEST maps
// project docs to their update triggers. When a verified worker edit matches
// a trigger glob, the runtime records the doc into state.md doc_debt — the
// Docs Lead resolves the batch at 6b, unresolved debt blocks the release.
//
// MANIFEST row format (markdown table):
//
//	| `docs/api/README.md` | Backend | OpenAPI change (`.orchestra/contract/OpenAPI*`) | draft |
//
// Backtick-quoted patterns inside the trigger cell are path globs
// (* = within a segment, ** = across segments). Free-text triggers without
// globs are for humans/L4 and are ignored by the runtime.

const docsManifestRel = ".orchestra/docs/MANIFEST.md"

type manifestRow struct {
	docPath string
	globs   []*regexp.Regexp
}

var backtickRe = regexp.MustCompile("`([^`]+)`")

// loadDocsManifest parses MANIFEST table rows that carry trigger globs.
func loadDocsManifest(projectRoot string) []manifestRow {
	data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(docsManifestRel)))
	if err != nil {
		return nil
	}
	var rows []manifestRow
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(t, "|"), "|")
		if len(cells) < 3 {
			continue
		}
		pathCell := strings.TrimSpace(cells[0])
		m := backtickRe.FindStringSubmatch(pathCell)
		if m == nil {
			continue // header/separator rows
		}
		docPath := strings.TrimSpace(m[1])
		var globs []*regexp.Regexp
		for _, g := range backtickRe.FindAllStringSubmatch(cells[2], -1) {
			if re := globToRegexp(strings.TrimSpace(g[1])); re != nil {
				globs = append(globs, re)
			}
		}
		if docPath != "" && len(globs) > 0 {
			rows = append(rows, manifestRow{docPath: docPath, globs: globs})
		}
	}
	return rows
}

// globToRegexp compiles a path glob: ** crosses segments, * stays inside one.
func globToRegexp(glob string) *regexp.Regexp {
	glob = strings.TrimPrefix(filepath.ToSlash(glob), "./")
	if glob == "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case glob[i] == '*':
			b.WriteString("[^/]*")
		case glob[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

// recordDocDebt matches verified worker edits against MANIFEST triggers and
// appends hits to state.md doc_debt. Editing the doc itself is not debt.
// Returns the doc paths recorded (for logs/tests). Best-effort: doc debt
// accounting must never fail a green worker result.
func recordDocDebt(projectRoot string, editedPaths []string) []string {
	if len(editedPaths) == 0 {
		return nil
	}
	rows := loadDocsManifest(projectRoot)
	if len(rows) == 0 {
		return nil
	}
	norm := make([]string, 0, len(editedPaths))
	for _, p := range editedPaths {
		norm = append(norm, strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(p)), "./"))
	}
	var recorded []string
	for _, row := range rows {
		hit := false
		for _, p := range norm {
			if p == row.docPath {
				hit = false // the doc itself was updated — no debt
				break
			}
			for _, re := range row.globs {
				if re.MatchString(p) {
					hit = true
					break
				}
			}
		}
		if hit {
			if err := orchestrastate.AddDocDebt(projectRoot, row.docPath); err == nil {
				recorded = append(recorded, row.docPath)
			}
		}
	}
	return recorded
}
