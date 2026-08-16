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
	leadPlaybooksInjectMaxBytes = 1800
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

// FormatLeadPlaybooksInject concatenates L2 playbooks + local overlays for
// Orchestra/Architecture Lead prompts so codebase rules persist across sessions.
func FormatLeadPlaybooksInject(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	depts := listPlaybookDepts(projectRoot)
	if len(depts) == 0 {
		return ""
	}
	remaining := leadPlaybooksInjectMaxBytes
	var b strings.Builder
	b.WriteString("<dept_playbooks>\n")
	wrote := false
	for _, dept := range depts {
		if remaining <= 80 {
			b.WriteString("…(more playbooks truncated; read .orchestra/playbooks/)\n")
			break
		}
		chunk := FormatDeptPlaybookInject(projectRoot, dept)
		if chunk == "" {
			continue
		}
		if len(chunk) > remaining {
			chunk = chunk[:remaining] + "\n...(truncated)"
		}
		b.WriteString(chunk)
		b.WriteByte('\n')
		remaining -= len(chunk)
		wrote = true
	}
	if !wrote {
		return ""
	}
	b.WriteString("</dept_playbooks>")
	return b.String()
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
