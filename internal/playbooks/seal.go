package playbooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// TrySealAllPendingOverlays scans local overlays and replaces PENDING decision_ref
// values when a matching User approval appears in decisions.md (Question Barrier).
func TrySealAllPendingOverlays(projectRoot string) []string {
	if projectRoot == "" {
		return nil
	}
	log := readDecisionLog(projectRoot)
	dir := filepath.Join(projectRoot, filepath.FromSlash(LocalRelDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var sealed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		dept := strings.TrimSuffix(e.Name(), ".md")
		if ok, _ := SealPendingOverlayFromLog(projectRoot, dept, log); ok {
			sealed = append(sealed, dept)
		}
	}
	return sealed
}

// SealPendingOverlayFromLog updates a draft overlay frontmatter when the decision
// log contains a User answer that approves the pending summary.
func SealPendingOverlayFromLog(projectRoot, dept, decisionLog string) (bool, error) {
	dept = lessons.NormalizeDept(dept)
	body, err := readLocalOverlayFile(projectRoot, dept)
	if err != nil {
		return false, err
	}
	ref := ParseDecisionRef(body)
	if ref == "" || !IsPendingDecisionRef(ref) {
		return false, nil
	}
	approval := findOverlayApproval(ref, decisionLog)
	if approval == "" {
		return false, nil
	}
	updated, err := rewriteOverlayDecisionRef(body, approval)
	if err != nil {
		return false, err
	}
	abs := filepath.Join(projectRoot, filepath.FromSlash(LocalOverlayRel(dept)))
	if err := fsutil.AtomicWriteFile(abs, []byte(updated), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func findOverlayApproval(pendingRef, decisionLog string) string {
	pendingRef = strings.TrimSpace(pendingRef)
	summary := strings.TrimSpace(strings.TrimPrefix(pendingRef, pendingDecisionPrefix))
	summary = strings.TrimSpace(summary)
	log := strings.ReplaceAll(decisionLog, "\r\n", "\n")
	for _, ln := range strings.Split(log, "\n") {
		ln = strings.TrimSpace(ln)
		const answerPrefix = "- A: "
		if !strings.HasPrefix(ln, answerPrefix) {
			continue
		}
		ans := strings.TrimSpace(strings.TrimPrefix(ln, answerPrefix))
		if ans == "" {
			continue
		}
		if summary != "" && strings.Contains(strings.ToLower(ans), strings.ToLower(summary)) {
			return ans
		}
		if isAffirmativeApproval(ans) && summary != "" && logMentionsOverlayApproval(log, summary) {
			return ans
		}
	}
	return ""
}

func isAffirmativeApproval(ans string) bool {
	lower := strings.ToLower(strings.TrimSpace(ans))
	switch lower {
	case "yes", "y", "approve", "approved", "ok", "lgtm", "go ahead", "confirm", "confirmed":
		return true
	}
	return strings.HasPrefix(lower, "approve ") || strings.HasPrefix(lower, "yes,")
}

func logMentionsOverlayApproval(log, summary string) bool {
	lowerLog := strings.ToLower(log)
	if strings.Contains(lowerLog, "playbook") || strings.Contains(lowerLog, "overlay") ||
		strings.Contains(lowerLog, "local learn") || strings.Contains(lowerLog, "lesson_promote") {
		return true
	}
	if summary != "" && strings.Contains(lowerLog, strings.ToLower(summary)) {
		return true
	}
	return false
}

func rewriteOverlayDecisionRef(body, decisionRef string) (string, error) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(body, "---\n") {
		return "", fmt.Errorf("overlay missing YAML frontmatter")
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", fmt.Errorf("overlay frontmatter not closed")
	}
	yamlPart := rest[:end]
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(yamlPart), &fm); err != nil {
		return "", err
	}
	fm["decision_ref"] = decisionRef
	fm["status"] = "approved"
	outYAML, err := yaml.Marshal(fm)
	if err != nil {
		return "", err
	}
	tail := rest[end+len("\n---"):]
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(outYAML)
	b.WriteString("---")
	b.WriteString(tail)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// DeptsNeedingPromoteHint lists depts whose approved local overlay is not yet merged to L2.
func DeptsNeedingPromoteHint(projectRoot string) []string {
	if projectRoot == "" {
		return nil
	}
	dir := filepath.Join(projectRoot, filepath.FromSlash(LocalRelDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	log := readDecisionLog(projectRoot)
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		dept := lessons.NormalizeDept(strings.TrimSuffix(e.Name(), ".md"))
		body, err := readLocalOverlayFile(projectRoot, dept)
		if err != nil {
			continue
		}
		if !NeedsPlaybookPromoteHint(projectRoot, dept, body, log) {
			continue
		}
		out = append(out, dept)
	}
	return out
}

// NeedsPlaybookPromoteHint reports whether Lead should call playbook_promote next.
func NeedsPlaybookPromoteHint(projectRoot, dept, localBody, decisionLog string) bool {
	ref := ParseDecisionRef(localBody)
	if ref == "" || IsPendingDecisionRef(ref) {
		return false
	}
	if !LocalOverlayApproved(localBody, decisionLog) {
		return false
	}
	section := extractOverlayBody(localBody)
	if section == "" {
		return false
	}
	l2, _ := readFirstPlaybook(projectRoot, dept)
	if l2 == "" {
		return true
	}
	key := strings.TrimSpace(strings.Split(section, "\n")[0])
	if key == "" {
		return true
	}
	return !strings.Contains(l2, key)
}

// FormatPlaybookPromoteHint returns Lead guidance when an approved overlay awaits L2 merge.
func FormatPlaybookPromoteHint(dept string) string {
	dept = lessons.NormalizeDept(dept)
	return fmt.Sprintf(
		"Approved local overlay for dept %q is ready — ask the User to approve merging into L2 playbook, record the exact promotion_ref in decisions.md, then playbook_promote{\"dept\":%q,\"promotion_ref\":\"<exact approval text>\"}",
		dept, dept,
	)
}
