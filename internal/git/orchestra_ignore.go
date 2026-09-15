package git

import "strings"

// OrchestraIgnorePatterns is the ignore rule set for Orchestra's own files: the
// secrets file, run logs, the CKG database and local-only plans are ignored,
// while the project knowledge under .orchestra/ (state, decisions, plans, specs,
// playbooks, product and docs) stays tracked.
//
// It has two readers and must stay one list. `orchestra init` writes it into
// the project's .gitignore; git.commit applies it to `add: ["."]` whether or
// not init was ever run, because the model's own commit is the path that would
// otherwise put ckg.db, llm_log.jsonl and .orchestra.local.yml into history.
//
// Patterns are relative to the project root, the way a .gitignore there reads
// them.
const OrchestraIgnorePatterns = `.orchestra.local.yml
ORCHESTRA.local.md
*.orchestra.bak
*.bak
*.tmp
.orchestra/*
.orchestra/*.db*
!.orchestra/state.md
!.orchestra/decisions.md
!.orchestra/system.txt
!.orchestra/plans/
.orchestra/plans/local/
!.orchestra/specs/
!.orchestra/playbooks/
.orchestra/playbooks/local/
!.orchestra/product/
!.orchestra/docs/
`

// AnchorIgnorePatterns rewrites project-root-relative patterns so they mean the
// same thing to `git ls-files --exclude-from`, which anchors patterns with a
// slash at the TOP of the repository rather than at the project root.
// prefix is `git rev-parse --show-prefix` for the project root ("" when the
// project is the repository root, "services/api/" inside a monorepo).
//
// A pattern with no slash, or only a trailing one, floats — it matches at any
// depth — and is left as it is.
func AnchorIgnorePatterns(patterns, prefix string) string {
	var b strings.Builder
	for _, line := range strings.Split(patterns, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		neg := ""
		if strings.HasPrefix(trimmed, "!") {
			neg, trimmed = "!", trimmed[1:]
		}
		if strings.Contains(strings.TrimSuffix(trimmed, "/"), "/") {
			trimmed = "/" + prefix + strings.TrimPrefix(trimmed, "/")
		}
		b.WriteString(neg + trimmed + "\n")
	}
	return b.String()
}
