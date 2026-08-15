package playbooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// MergeApprovedLocalToL2 appends an approved local overlay into the dept L2 playbook.
// promotionRef must appear verbatim in decisions.md (User approved the merge).
func MergeApprovedLocalToL2(projectRoot, dept, promotionRef, decisionLog string) (l2Rel string, err error) {
	dept = lessons.NormalizeDept(dept)
	promotionRef = strings.TrimSpace(promotionRef)
	if promotionRef == "" {
		return "", fmt.Errorf("promotion_ref must not be empty")
	}
	if !strings.Contains(decisionLog, promotionRef) {
		return "", fmt.Errorf("promotion_ref %q not found in decisions log — ask the User and record approval first", promotionRef)
	}
	localBody, err := readLocalOverlayFile(projectRoot, dept)
	if err != nil {
		return "", err
	}
	if !LocalOverlayApproved(localBody, decisionLog) {
		return "", fmt.Errorf("local overlay is not approved — set decision_ref in frontmatter to text present in decisions.md first")
	}
	section := extractOverlayBody(localBody)
	if section == "" {
		return "", fmt.Errorf("local overlay has no content to merge")
	}

	l2Rel = filepath.ToSlash(filepath.Join(".orchestra", "playbooks", dept+".md"))
	l2Abs := filepath.Join(projectRoot, filepath.FromSlash(l2Rel))
	existing, _ := os.ReadFile(l2Abs)
	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(strings.TrimSpace(string(existing)), "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	} else {
		b.WriteString("# ")
		b.WriteString(dept)
		b.WriteString(" playbook (L2)\n\n")
	}
	ts := time.Now().UTC().Format("2006-01-02")
	fmt.Fprintf(&b, "## Merged local learnings · %s\n", ts)
	fmt.Fprintf(&b, "<!-- promotion_ref: %s -->\n\n", promotionRef)
	b.WriteString(section)
	b.WriteByte('\n')

	if err := os.MkdirAll(filepath.Dir(l2Abs), 0o755); err != nil {
		return "", err
	}
	if err := fsutil.AtomicWriteFile(l2Abs, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return l2Rel, nil
}

func readLocalOverlayFile(root, dept string) (string, error) {
	abs := filepath.Join(root, filepath.FromSlash(LocalRelDir), dept+".md")
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("local overlay %q does not exist", LocalOverlayRel(dept))
		}
		return "", err
	}
	return string(data), nil
}

func extractOverlayBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if strings.HasPrefix(body, "---\n") {
		rest := body[len("---\n"):]
		if end := strings.Index(rest, "\n---"); end >= 0 {
			return strings.TrimSpace(rest[end+len("\n---"):])
		}
	}
	return strings.TrimSpace(body)
}
