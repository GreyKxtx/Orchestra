package playbooks

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/lessons"
)

const (
	// LocalRelDir holds runtime-learned playbook overlays (gitignored under .orchestra/).
	LocalRelDir = ".orchestra/playbooks/local"

	deptPlaybookInjectMaxBytes  = 2048
	leadPlaybooksInjectMaxBytes = 4000 // ~1000 tokens for active dept + overlay
)

// FormatDeptPlaybookInject returns an XML block with the dept L2 playbook and
// optional local overlay merged for worker spawn. Empty when nothing exists.
func FormatDeptPlaybookInject(projectRoot, dept string) string {
	if projectRoot == "" {
		return ""
	}
	dept = lessons.NormalizeDept(dept)
	base, baseRel := readFirstPlaybook(projectRoot, dept)
	local, localRel := readLocalOverlay(projectRoot, dept)
	if strings.TrimSpace(base) == "" && strings.TrimSpace(local) == "" {
		return ""
	}
	decisionLog := readDecisionLog(projectRoot)
	localApproved := LocalOverlayApproved(local, decisionLog)
	var b strings.Builder
	b.WriteString("<dept_playbook")
	if baseRel != "" {
		b.WriteString(` source="`)
		b.WriteString(baseRel)
		b.WriteByte('"')
	}
	if localRel != "" {
		b.WriteString(` local_overlay="`)
		b.WriteString(localRel)
		b.WriteByte('"')
	}
	b.WriteString(">\n")
	if body := trimInject(base); body != "" {
		b.WriteString(body)
	}
	if body := trimInject(local); body != "" {
		if b.Len() > len("<dept_playbook>\n") {
			ref := ParseDecisionRef(local)
			switch {
			case localApproved:
				b.WriteString("\n\n--- local overlay (approved via decisions.md) ---\n")
			case IsPendingDecisionRef(ref):
				b.WriteString("\n\n--- local overlay (draft; await User approval) ---\n")
			default:
				b.WriteString("\n\n--- local overlay (runtime; pending User approval) ---\n")
			}
		}
		b.WriteString(body)
	}
	b.WriteString("\n</dept_playbook>")
	return b.String()
}

// FormatLeadPlaybooksInject injects the active dept L2 playbook + local overlay
// (≤ leadPlaybooksInjectMaxBytes). When activeDept is empty, only a filename
// index is emitted so the Lead can memory_read / read the file on demand.
func FormatLeadPlaybooksInject(projectRoot, activeDept string) string {
	if projectRoot == "" {
		return ""
	}
	activeDept = strings.TrimSpace(activeDept)
	if activeDept != "" {
		chunk := FormatDeptPlaybookInject(projectRoot, activeDept)
		if chunk == "" {
			return ""
		}
		if len(chunk) > leadPlaybooksInjectMaxBytes {
			chunk = chunk[:leadPlaybooksInjectMaxBytes] + "\n...(truncated; read .orchestra/playbooks/)"
		}
		return "<dept_playbooks>\n" + chunk + "\n</dept_playbooks>"
	}
	depts := listPlaybookDepts(projectRoot)
	if len(depts) == 0 {
		return ""
	}
	return "<dept_playbooks>\navailable: " + strings.Join(depts, ", ") +
		"\n(read a dept file via memory_read / read .orchestra/playbooks/{dept}.md)\n</dept_playbooks>"
}

func listPlaybookDepts(root string) []string {
	seen := make(map[string]struct{})
	addDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if e.Name() == "conventions.md" {
				continue
			}
			dept := lessons.NormalizeDept(strings.TrimSuffix(e.Name(), ".md"))
			seen[dept] = struct{}{}
		}
	}
	addDir(filepath.Join(root, ".orchestra", "playbooks"))
	addDir(filepath.Join(root, filepath.FromSlash(LocalRelDir)))
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func readFirstPlaybook(root, dept string) (body, rel string) {
	candidates := []string{dept + ".md"}
	if i := strings.Index(dept, "@"); i > 0 {
		candidates = append(candidates, dept[:i]+".md")
	}
	dir := filepath.Join(root, ".orchestra", "playbooks")
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		body = strings.TrimSpace(string(data))
		if body != "" {
			return body, filepath.ToSlash(filepath.Join(".orchestra", "playbooks", name))
		}
	}
	return "", ""
}

func readLocalOverlay(root, dept string) (body, rel string) {
	path := filepath.Join(root, filepath.FromSlash(LocalRelDir), dept+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	body = strings.TrimSpace(string(data))
	if body == "" {
		return "", ""
	}
	return body, filepath.ToSlash(filepath.Join(LocalRelDir, dept+".md"))
}

func trimInject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= deptPlaybookInjectMaxBytes {
		return s
	}
	return s[:deptPlaybookInjectMaxBytes] + "\n...(truncated)"
}

func readDecisionLog(root string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)))
	if err != nil {
		return ""
	}
	return string(data)
}
