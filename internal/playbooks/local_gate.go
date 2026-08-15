package playbooks

import (
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/orchestra/orchestra/internal/lessons"
)

// ParseLocalOverlayPath reports whether path is .orchestra/playbooks/local/{dept}.md.
func ParseLocalOverlayPath(path string) (dept string, ok bool) {
	p := normalizeRelPath(path)
	prefix := LocalRelDir + "/"
	if !strings.HasPrefix(p, prefix) || !strings.HasSuffix(p, ".md") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(p, prefix), ".md")
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	if !lessons.IsDeptScope(name) {
		return "", false
	}
	return lessons.NormalizeDept(name), true
}

// LocalOverlayRel returns the project-relative path for a dept local overlay file.
func LocalOverlayRel(dept string) string {
	dept = lessons.NormalizeDept(dept)
	return filepath.ToSlash(filepath.Join(LocalRelDir, dept+".md"))
}

// ParseDecisionRef extracts decision_ref from YAML frontmatter.
func ParseDecisionRef(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	yamlPart := body
	if strings.HasPrefix(body, "---\n") {
		rest := body[len("---\n"):]
		if end := strings.Index(rest, "\n---"); end >= 0 {
			yamlPart = rest[:end]
		}
	}
	var fm struct {
		DecisionRef string `yaml:"decision_ref"`
	}
	if err := yaml.Unmarshal([]byte(yamlPart), &fm); err != nil {
		return ""
	}
	return strings.TrimSpace(fm.DecisionRef)
}

// LocalOverlayApproved reports whether body carries a decision_ref present in the log.
func LocalOverlayApproved(body, decisionLog string) bool {
	ref := ParseDecisionRef(body)
	if ref == "" || strings.TrimSpace(decisionLog) == "" {
		return false
	}
	return strings.Contains(decisionLog, ref)
}

func normalizeRelPath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "./")
	for strings.HasPrefix(p, "../") {
		p = strings.TrimPrefix(p, "../")
	}
	return p
}
