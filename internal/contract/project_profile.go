package contract

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// project_profile (spec §2.3, §3.5, checklist 18): a PRD frontmatter key that
// tunes playbook strictness and gate requirements without changing the org
// chart. The runtime reads it fail-open: a missing PRD or profile means
// "default".

// PRDFileRel is where the Product Lead writes the PRD.
const PRDFileRel = ".orchestra/product/PRD.md"

// Known profile values (spec §2.3 table). `ml` and `embedded` are explicitly
// out of scope.
const (
	ProfileDefault      = "default"
	ProfileMobile       = "mobile"
	ProfileDataPlatform = "data_platform"
	ProfileEnterprise   = "enterprise"
	ProfileRealtime     = "realtime"
)

// ProjectProfile reads project_profile from the PRD frontmatter.
// Missing file / frontmatter / key → "default".
func ProjectProfile(projectRoot string) string {
	data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(PRDFileRel)))
	if err != nil {
		return ProfileDefault
	}
	body := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(body, "---\n") {
		return ProfileDefault
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ProfileDefault
	}
	var fm struct {
		ProjectProfile string `yaml:"project_profile"`
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return ProfileDefault
	}
	p := strings.ToLower(strings.TrimSpace(fm.ProjectProfile))
	if p == "" {
		return ProfileDefault
	}
	return p
}

// enterpriseNFRSections are the sections NFR.md must carry under
// project_profile: enterprise (spec §3.5 exceptions table).
var enterpriseNFRSections = []string{"compliance", "data residency"}

// checkEnterpriseNFR enforces the enterprise profile requirement on NFR.md:
// compliance and data-residency sections must exist as headings.
func checkEnterpriseNFR(data []byte) string {
	headings := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") {
			headings[strings.ToLower(strings.TrimSpace(strings.TrimLeft(t, "# ")))] = true
		}
	}
	var missing []string
	for _, want := range enterpriseNFRSections {
		found := false
		for h := range headings {
			if strings.Contains(h, want) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return "project_profile: enterprise requires NFR sections: " + strings.Join(missing, ", ")
	}
	return ""
}
