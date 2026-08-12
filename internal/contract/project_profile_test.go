package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePRD(t *testing.T, root, frontmatter string) {
	t.Helper()
	dir := filepath.Join(root, ".orchestra", "product")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\n" + frontmatter + "---\n\n# PRD\n"
	if err := os.WriteFile(filepath.Join(dir, "PRD.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectProfile(t *testing.T) {
	root := t.TempDir()
	if got := ProjectProfile(root); got != ProfileDefault {
		t.Fatalf("missing PRD → default, got %q", got)
	}
	writePRD(t, root, "status: approved\nproject_profile: Enterprise\n")
	if got := ProjectProfile(root); got != ProfileEnterprise {
		t.Fatalf("profile = %q, want enterprise", got)
	}
	writePRD(t, root, "status: approved\n")
	if got := ProjectProfile(root); got != ProfileDefault {
		t.Fatalf("missing key → default, got %q", got)
	}
}

func writeContractArtifacts(t *testing.T, root, nfr string) {
	t.Helper()
	dir := filepath.Join(root, ".orchestra", "contract")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		ArtifactDomainModel: "# Domain\n\n## User\nan account\n",
		ArtifactNFR:         nfr,
		ArtifactOpenAPI:     "openapi: 3.1.0\npaths:\n  /users:\n    get: {}\n",
		ArtifactUITokens:    `{"color":{"primary":"#000"}}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyArtifacts_EnterpriseNFR(t *testing.T) {
	root := t.TempDir()
	plainNFR := "# NFR\n\n## Latency\np95 < 200ms\n"
	writeContractArtifacts(t, root, plainNFR)

	// default profile: plain NFR passes.
	if issues := VerifyArtifacts(root); len(issues) != 0 {
		t.Fatalf("default profile must pass: %v", issues)
	}

	// enterprise: compliance + data residency sections required.
	writePRD(t, root, "status: approved\nproject_profile: enterprise\n")
	issues := VerifyArtifacts(root)
	if len(issues) != 1 || !strings.Contains(issues[0], "compliance") || !strings.Contains(issues[0], "data residency") {
		t.Fatalf("enterprise must demand compliance sections, got %v", issues)
	}

	entNFR := "# NFR\n\n## Latency\np95 < 200ms\n\n## Compliance\nSOC2 type II\n\n## Data residency\nEU only\n"
	writeContractArtifacts(t, root, entNFR)
	if issues := VerifyArtifacts(root); len(issues) != 0 {
		t.Fatalf("enterprise NFR with sections must pass: %v", issues)
	}
}
