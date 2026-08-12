package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/orchestra/orchestra/internal/orchestrastate"
)

// Brief completeness gate (spec §6.2, checklist 15): a Lead must not spawn
// workers while its Implementation Brief misses required sections. The gate
// is opt-in per department: it activates only when the dept's L2 playbook
// frontmatter declares brief_required_fields[]. The dept binding comes from
// the WorkOrder's context.scratchpad (.orchestra/depts/{instance}.md — the
// same binding the scratchpad auto-append uses).

const (
	playbooksRelDir = ".orchestra/playbooks"
	specsRelDir     = ".orchestra/specs"
)

// workOrderDeptInstance extracts the department instance from the WorkOrder
// scratchpad binding ("" when the WorkOrder is not dept-bound).
func workOrderDeptInstance(wo *WorkOrder) string {
	if wo == nil || wo.Context == nil {
		return ""
	}
	sp, _ := wo.Context["scratchpad"].(string)
	sp = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(sp)), "./")
	rest, ok := strings.CutPrefix(sp, ".orchestra/depts/")
	if !ok || !strings.HasSuffix(rest, ".md") || strings.Contains(rest, "/") {
		return ""
	}
	return strings.TrimSuffix(rest, ".md")
}

// deptType strips the instance suffix: frontend@web → frontend.
func deptType(instance string) string {
	if i := strings.Index(instance, "@"); i > 0 {
		return instance[:i]
	}
	return instance
}

// checkBriefCompleteness is the runtime gate evaluated at worker spawn.
// Fail-closed once the playbook opts in; inactive in maintenance and under
// the user waiver `brief_completeness`.
func checkBriefCompleteness(projectRoot string, wo *WorkOrder) error {
	instance := workOrderDeptInstance(wo)
	if instance == "" {
		return nil
	}
	required, playbookRel := briefRequiredFields(projectRoot, instance)
	if len(required) == 0 {
		return nil
	}
	st, found, err := orchestrastate.Load(projectRoot)
	if err == nil && found {
		if st.Phase == orchestrastate.PhaseMaintenance || st.HasWaiver(orchestrastate.WaiverBriefCompleteness) {
			return nil
		}
	}
	briefRel, briefBody := findBrief(projectRoot, instance)
	if briefRel == "" {
		return fmt.Errorf("runtime_guard: brief_completeness — %s declares brief_required_fields but no Implementation Brief found under %s/%s/; "+
			"unblock: Lead writes the brief (brief.md) | user waiver 'brief_completeness'",
			playbookRel, specsRelDir, instance)
	}
	var missing []string
	for _, field := range required {
		if !briefHasSection(briefBody, field) {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("runtime_guard: brief_completeness — %s misses required non-empty sections %q (declared in %s); "+
			"unblock: fill the sections (open_questions → barrier, or assumptions[]) | user waiver 'brief_completeness'",
			briefRel, missing, playbookRel)
	}
	return nil
}

// briefRequiredFields reads brief_required_fields from the instance playbook,
// falling back to the dept-type playbook (frontend@web.md → frontend.md).
func briefRequiredFields(projectRoot, instance string) (fields []string, playbookRel string) {
	for _, name := range playbookCandidates(instance) {
		rel := playbooksRelDir + "/" + name + ".md"
		data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		if f := parseBriefRequiredFields(string(data)); len(f) > 0 {
			return f, rel
		}
		return nil, rel // playbook exists but does not opt in
	}
	return nil, ""
}

func playbookCandidates(instance string) []string {
	if dt := deptType(instance); dt != instance {
		return []string{instance, dt}
	}
	return []string{instance}
}

func parseBriefRequiredFields(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	yamlPart := body
	if strings.HasPrefix(body, "---\n") {
		rest := body[len("---\n"):]
		if end := strings.Index(rest, "\n---"); end >= 0 {
			yamlPart = rest[:end]
		}
	}
	var fm struct {
		BriefRequiredFields []string `yaml:"brief_required_fields"`
	}
	if err := yaml.Unmarshal([]byte(yamlPart), &fm); err != nil {
		return nil
	}
	out := fm.BriefRequiredFields[:0]
	for _, f := range fm.BriefRequiredFields {
		if strings.TrimSpace(f) != "" {
			out = append(out, strings.TrimSpace(f))
		}
	}
	return out
}

// findBrief locates the Implementation Brief for an instance:
// .orchestra/specs/{instance}/brief.md, then the dept-type variants.
func findBrief(projectRoot, instance string) (rel, body string) {
	for _, dir := range playbookCandidates(instance) {
		for _, name := range []string{"brief.md", "implementation_brief.md"} {
			r := specsRelDir + "/" + dir + "/" + name
			data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(r)))
			if err == nil {
				return r, string(data)
			}
		}
	}
	return "", ""
}

// briefHasSection reports whether the brief contains a non-empty "## <field>"
// section. Matching is loose: heading and field are normalized to
// lowercase alphanumeric tokens (api_contract_ref matches "## API contract ref").
func briefHasSection(body, field string) bool {
	want := normalizeSectionKey(field)
	if want == "" {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		heading := normalizeSectionKey(strings.TrimLeft(t, "# "))
		if heading != want {
			continue
		}
		// Non-empty content until the next heading.
		for j := i + 1; j < len(lines); j++ {
			n := strings.TrimSpace(lines[j])
			if strings.HasPrefix(n, "#") {
				break
			}
			if n != "" && n != "---" {
				return true
			}
		}
	}
	return false
}

func normalizeSectionKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}
