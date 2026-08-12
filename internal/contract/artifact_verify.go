package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Built-in Artifact Verify (spec §5.4, ADR-4): machine checks that gate the
// contract → execution transition (G6). External linters (spectral) can
// replace the OpenAPI check later; these built-ins are the fail-closed floor.

// Canonical artifact file names inside .orchestra/contract/.
const (
	ArtifactDomainModel = "Domain_Model.md"
	ArtifactNFR         = "NFR.md"
	ArtifactOpenAPI     = "OpenAPI.v0.yaml"
	ArtifactUITokens    = "UI_Tokens.skeleton.json"
)

// RequiredArtifacts is the stage-2.5 freeze set.
var RequiredArtifacts = []string{ArtifactDomainModel, ArtifactNFR, ArtifactOpenAPI, ArtifactUITokens}

// VerifyArtifacts runs the built-in checks over .orchestra/contract/ and
// returns the list of issues (empty = green, freeze may proceed).
func VerifyArtifacts(projectRoot string) []string {
	var issues []string
	profile := ProjectProfile(projectRoot)
	dir := filepath.Join(projectRoot, filepath.FromSlash(DirRel))
	for _, name := range RequiredArtifacts {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: missing (%v)", name, err))
			continue
		}
		if msg := checkArtifact(name, data); msg != "" {
			issues = append(issues, fmt.Sprintf("%s: %s", name, msg))
			continue
		}
		// Profile overrides (spec §3.5): enterprise hardens the NFR gate.
		if name == ArtifactNFR && profile == ProfileEnterprise {
			if msg := checkEnterpriseNFR(data); msg != "" {
				issues = append(issues, fmt.Sprintf("%s: %s", name, msg))
			}
		}
	}
	return issues
}

func checkArtifact(name string, data []byte) string {
	switch name {
	case ArtifactDomainModel:
		if msg := checkMarkdownSections(data); msg != "" {
			return msg
		}
		return checkDomainDuplicates(data)
	case ArtifactNFR:
		return checkMarkdownSections(data)
	case ArtifactOpenAPI:
		return checkOpenAPI(data)
	case ArtifactUITokens:
		return checkUITokens(data)
	}
	return ""
}

// checkDomainDuplicates rejects duplicate entity names in Domain_Model.md
// ("## Entity" headings, case-insensitive) — spec §5.4 domain check. Semantic
// coverage against User_Stories.md stays with the L4 verifier.
func checkDomainDuplicates(data []byte) string {
	seen := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "## ") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(t, "## "))
		key := strings.ToLower(name)
		if key == "" {
			continue
		}
		if first, dup := seen[key]; dup {
			return fmt.Sprintf("duplicate entity name: %q and %q", first, name)
		}
		seen[key] = name
	}
	return ""
}

// checkMarkdownSections requires non-empty content with at least one section
// heading — an empty skeleton must not pass the freeze gate.
func checkMarkdownSections(data []byte) string {
	body := strings.TrimSpace(string(data))
	if body == "" {
		return "empty file"
	}
	hasSection := false
	hasContent := false
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") {
			hasSection = true
			continue
		}
		if t != "" && !strings.HasPrefix(t, "#") && t != "---" {
			hasContent = true
		}
	}
	if !hasSection {
		return "no '## ' sections — required sections must be present and non-empty"
	}
	if !hasContent {
		return "sections are empty — headings without content do not pass the freeze gate"
	}
	return ""
}

// checkOpenAPI validates YAML shape: parseable, has an openapi version and a
// non-empty paths (or components) map. Full lint belongs to spectral.
func checkOpenAPI(data []byte) string {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Sprintf("invalid YAML: %v", err)
	}
	ver, _ := doc["openapi"].(string)
	if strings.TrimSpace(ver) == "" {
		return "missing top-level 'openapi' version field"
	}
	paths, hasPaths := doc["paths"].(map[string]any)
	comps, hasComps := doc["components"].(map[string]any)
	if (!hasPaths || len(paths) == 0) && (!hasComps || len(comps) == 0) {
		return "neither paths nor components defined — empty contract"
	}
	return ""
}

// checkUITokens validates the token skeleton: JSON object with at least one
// non-empty token group.
func checkUITokens(data []byte) string {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Sprintf("invalid JSON: %v", err)
	}
	if len(doc) == 0 {
		return "empty token object — at least one token group required"
	}
	return ""
}

// DefaultOwners is the spec §5.3 artifact ownership map used by the initial
// freeze when no explicit owners are supplied.
var DefaultOwners = map[string]string{
	ArtifactDomainModel: "backend",
	ArtifactNFR:         "orchestrator",
	ArtifactOpenAPI:     "backend",
	ArtifactUITokens:    "design",
}

// FreezeAll hashes every required artifact into EPOCH.yaml (initial freeze,
// stage 2.5). Fails if Artifact Verify is red — the gate is fail-closed.
func FreezeAll(projectRoot string, owners map[string]string) (*Epoch, error) {
	if issues := VerifyArtifacts(projectRoot); len(issues) > 0 {
		return nil, fmt.Errorf("artifact verify failed:\n  %s", strings.Join(issues, "\n  "))
	}
	var e *Epoch
	var err error
	for _, name := range RequiredArtifacts {
		e, err = UpdateArtifact(projectRoot, name, owners[name])
		if err != nil {
			return nil, err
		}
	}
	return e, nil
}
