package playbooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/patch/fsutil"
)

const pendingDecisionPrefix = "PENDING:"

// DraftLocalOverlay writes a pending local overlay draft (no User approval yet).
// decision_ref is set to PENDING:<summary> until the User approves in decisions.md.
func DraftLocalOverlay(projectRoot, dept, body, source, pendingSummary string) (relPath string, err error) {
	dept = lessons.NormalizeDept(dept)
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("overlay body must not be empty")
	}
	pendingSummary = strings.TrimSpace(pendingSummary)
	if pendingSummary == "" {
		pendingSummary = "local playbook promotion"
	}
	if len(pendingSummary) > 160 {
		pendingSummary = pendingSummary[:159] + "…"
	}
	ref := pendingDecisionPrefix + " " + pendingSummary
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "decision_ref: %q\n", ref)
	fmt.Fprintf(&b, "status: draft\n")
	if source != "" {
		fmt.Fprintf(&b, "source: %q\n", source)
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	b.WriteByte('\n')

	dir := filepath.Join(projectRoot, filepath.FromSlash(LocalRelDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	abs := filepath.Join(dir, dept+".md")
	if err := fsutil.AtomicWriteFile(abs, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return LocalOverlayRel(dept), nil
}

// IsPendingDecisionRef reports draft overlays awaiting User approval.
func IsPendingDecisionRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), pendingDecisionPrefix)
}

// DraftFromLastPattern promotes the latest pattern lesson to a pending local overlay.
func DraftFromLastPattern(projectRoot, dept string) (relPath, pendingRef string, err error) {
	entry, ok := lessons.LastEntryOfKind(projectRoot, dept, lessons.KindPattern)
	if !ok {
		return "", "", fmt.Errorf("no pattern lesson for dept %q", lessons.NormalizeDept(dept))
	}
	body := lessons.FormatPatternForPromote(entry)
	summary := strings.TrimSpace(entry.Task)
	if summary == "" {
		summary = "promote last pattern lesson"
	}
	rel, err := DraftLocalOverlay(projectRoot, dept, body, lessons.RelDir+"/"+lessons.NormalizeDept(dept)+".md", summary)
	if err != nil {
		return "", "", err
	}
	return rel, pendingDecisionPrefix + " " + summary, nil
}
