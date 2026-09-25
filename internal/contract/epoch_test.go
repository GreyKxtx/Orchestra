package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/wsview"
)

func writeArtifact(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(DirRel), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingEpoch(t *testing.T) {
	e, found, err := Load(t.TempDir())
	if err != nil || found || e != nil {
		t.Fatalf("missing epoch: e=%v found=%v err=%v", e, found, err)
	}
}

func TestUpdateArtifactBumpsVersionAndEpoch(t *testing.T) {
	root := t.TempDir()
	writeArtifact(t, root, ArtifactNFR, "## Latency\np95 < 200ms\n")

	e, err := UpdateArtifact(root, wsview.Disk(root), ArtifactNFR, "orchestrator")
	if err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	if e.Epoch != 1 || e.Artifacts[ArtifactNFR].Version != 1 {
		t.Fatalf("epoch=%d version=%d, want 1/1", e.Epoch, e.Artifacts[ArtifactNFR].Version)
	}
	if e.Artifacts[ArtifactNFR].Owner != "orchestrator" {
		t.Fatalf("owner = %q", e.Artifacts[ArtifactNFR].Owner)
	}

	// Same content → idempotent, no bump.
	e, err = UpdateArtifact(root, wsview.Disk(root), ArtifactNFR, "")
	if err != nil {
		t.Fatalf("UpdateArtifact (same): %v", err)
	}
	if e.Epoch != 1 || e.Artifacts[ArtifactNFR].Version != 1 {
		t.Fatalf("idempotent update must not bump: epoch=%d version=%d", e.Epoch, e.Artifacts[ArtifactNFR].Version)
	}

	// Changed content → version++ and epoch++.
	writeArtifact(t, root, ArtifactNFR, "## Latency\np95 < 100ms\n")
	e, err = UpdateArtifact(root, wsview.Disk(root), ArtifactNFR, "")
	if err != nil {
		t.Fatalf("UpdateArtifact (changed): %v", err)
	}
	if e.Epoch != 2 || e.Artifacts[ArtifactNFR].Version != 2 {
		t.Fatalf("epoch=%d version=%d, want 2/2", e.Epoch, e.Artifacts[ArtifactNFR].Version)
	}

	// Path traversal in name rejected.
	if _, err := UpdateArtifact(root, wsview.Disk(root), "../evil.md", ""); err == nil {
		t.Fatal("artifact name with separators must be rejected")
	}
}

func TestVerifyRefs(t *testing.T) {
	root := t.TempDir()

	// Empty refs are always fine.
	if err := VerifyRefs(root, wsview.Disk(root), nil); err != nil {
		t.Fatalf("nil refs: %v", err)
	}

	// Refs without an EPOCH file — invented refs, rejected.
	if err := VerifyRefs(root, wsview.Disk(root), []Ref{{Path: ArtifactNFR, SHA256: "aa"}}); err == nil {
		t.Fatal("refs without EPOCH.yaml must fail")
	}

	writeArtifact(t, root, ArtifactNFR, "## Latency\np95 < 200ms\n")
	if _, err := UpdateArtifact(root, wsview.Disk(root), ArtifactNFR, "orchestrator"); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	e, _, _ := Load(root)
	good := e.Artifacts[ArtifactNFR].SHA256

	// Matching hash passes; full path and bare name both resolve.
	for _, p := range []string{ArtifactNFR, ".orchestra/contract/" + ArtifactNFR} {
		if err := VerifyRefs(root, wsview.Disk(root), []Ref{{Path: p, SHA256: good}}); err != nil {
			t.Fatalf("ref %q should pass: %v", p, err)
		}
	}
	// Case-insensitive hash compare.
	if err := VerifyRefs(root, wsview.Disk(root), []Ref{{Path: ArtifactNFR, SHA256: strings.ToUpper(good)}}); err != nil {
		t.Fatalf("hash compare must be case-insensitive: %v", err)
	}

	// Stale hash fails with stale_contract.
	err := VerifyRefs(root, wsview.Disk(root), []Ref{{Path: ArtifactNFR, SHA256: "deadbeef"}})
	if err == nil || !strings.Contains(err.Error(), "stale_contract") {
		t.Fatalf("stale hash: %v", err)
	}
	// Unknown artifact fails.
	if err := VerifyRefs(root, wsview.Disk(root), []Ref{{Path: "Nope.md", SHA256: good}}); err == nil {
		t.Fatal("unknown artifact must fail")
	}
	// Empty hash fails.
	if err := VerifyRefs(root, wsview.Disk(root), []Ref{{Path: ArtifactNFR, SHA256: ""}}); err == nil {
		t.Fatal("empty sha256 must fail")
	}
}

func TestVerifyArtifactsAndFreeze(t *testing.T) {
	root := t.TempDir()

	// All missing → 4 issues.
	if issues := VerifyArtifacts(wsview.Disk(root)); len(issues) != 4 {
		t.Fatalf("want 4 issues for empty dir, got %v", issues)
	}

	writeArtifact(t, root, ArtifactDomainModel, "# Domain\n\n## Booking\nid, user_id, status\n")
	writeArtifact(t, root, ArtifactNFR, "## Latency\np95 < 200ms\n\n## Availability\n99.9%\n")
	writeArtifact(t, root, ArtifactOpenAPI, "openapi: 3.1.0\npaths:\n  /bookings:\n    get: {}\n")
	writeArtifact(t, root, ArtifactUITokens, `{"color": {"primary": "#333"}}`)

	if issues := VerifyArtifacts(wsview.Disk(root)); len(issues) != 0 {
		t.Fatalf("expected green verify, got %v", issues)
	}

	e, err := FreezeAll(root, wsview.Disk(root), map[string]string{ArtifactNFR: "orchestrator", ArtifactOpenAPI: "backend"})
	if err != nil {
		t.Fatalf("FreezeAll: %v", err)
	}
	if len(e.Artifacts) != 4 || e.Epoch != 4 {
		t.Fatalf("freeze: epoch=%d artifacts=%d", e.Epoch, len(e.Artifacts))
	}
	if e.Artifacts[ArtifactOpenAPI].Owner != "backend" {
		t.Fatalf("owner not recorded: %+v", e.Artifacts[ArtifactOpenAPI])
	}

	// Degenerate artifacts are caught.
	writeArtifact(t, root, ArtifactOpenAPI, "openapi: 3.1.0\n") // no paths/components
	writeArtifact(t, root, ArtifactUITokens, `{}`)
	writeArtifact(t, root, ArtifactNFR, "# NFR\n")
	issues := VerifyArtifacts(wsview.Disk(root))
	if len(issues) != 3 {
		t.Fatalf("want 3 issues, got %v", issues)
	}
	if _, err := FreezeAll(root, wsview.Disk(root), nil); err == nil {
		t.Fatal("FreezeAll must fail on red Artifact Verify")
	}
}

// staged is a view with one artifact staged over the disk, as an agent's
// overlay is.
type staged struct {
	wsview.Disk
	files map[string]string
}

func (s staged) ReadFile(rel string) ([]byte, error) {
	if c, ok := s.files[rel]; ok {
		return []byte(c), nil
	}
	return s.Disk.ReadFile(rel)
}

// A ref is checked against the version the task sees. A ref copied from
// EPOCH.yaml for an artifact that has since moved on — staged by its owner,
// or gone with a dry-run turn — describes a contract the task does not have.
func TestVerifyRefs_AgainstTheVersionTheTaskSees(t *testing.T) {
	root := t.TempDir()
	writeArtifact(t, root, ArtifactNFR, "## Latency\np95 < 200ms\n")
	if _, err := UpdateArtifact(root, wsview.Disk(root), ArtifactNFR, "orchestrator"); err != nil {
		t.Fatal(err)
	}
	e, _, _ := Load(root)
	frozen := []Ref{{Path: ArtifactNFR, SHA256: e.Artifacts[ArtifactNFR].SHA256}}

	changed := staged{Disk: wsview.Disk(root), files: map[string]string{DirRel + "/" + ArtifactNFR: "## Latency\np95 < 50ms\n"}}
	if err := VerifyRefs(root, changed, frozen); err == nil || !strings.Contains(err.Error(), "stale_contract") {
		t.Fatalf("a ref to the frozen hash must fail where the artifact changed: %v", err)
	}
	now := []Ref{{Path: ArtifactNFR, SHA256: contentSHA256([]byte("## Latency\np95 < 50ms\n"))}}
	if err := VerifyRefs(root, changed, now); err != nil {
		t.Fatalf("a ref to the version the task sees passes: %v", err)
	}

	if err := os.Remove(filepath.Join(root, DirRel, ArtifactNFR)); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRefs(root, wsview.Disk(root), frozen); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("a frozen artifact that is gone fails the ref: %v", err)
	}
}

// Refresh records a staged change of an adopted contract and nothing else.
func TestRefresh(t *testing.T) {
	root := t.TempDir()
	writeArtifact(t, root, ArtifactNFR, "## Latency\np95 < 200ms\n")
	v := staged{Disk: wsview.Disk(root), files: map[string]string{DirRel + "/" + ArtifactNFR: "## Latency\np95 < 50ms\n"}}
	if moved, err := Refresh(root, v, []string{DirRel + "/" + ArtifactNFR}); err != nil || len(moved) != 0 {
		t.Fatalf("before a freeze nothing is recorded: %v %v", moved, err)
	}
	if _, err := UpdateArtifact(root, wsview.Disk(root), ArtifactNFR, "orchestrator"); err != nil {
		t.Fatal(err)
	}
	moved, err := Refresh(root, v, []string{"src/a.go", DirRel + "/" + ArtifactNFR, EpochFileRel})
	if err != nil || len(moved) != 1 || moved[0] != ArtifactNFR {
		t.Fatalf("the staged change moves NFR.md: %v %v", moved, err)
	}
	e, _, _ := Load(root)
	if e.Epoch != 2 || e.Artifacts[ArtifactNFR].SHA256 != contentSHA256([]byte("## Latency\np95 < 50ms\n")) {
		t.Fatalf("EPOCH.yaml records the staged version: %+v", e)
	}
	if moved, _ := Refresh(root, v, []string{DirRel + "/" + ArtifactNFR}); len(moved) != 0 {
		t.Fatalf("the same content again moves nothing: %v", moved)
	}
}

func TestOwnedBy(t *testing.T) {
	for _, c := range []struct {
		rel, owner string
		want       bool
	}{
		{DirRel + "/" + ArtifactOpenAPI, "backend", true},
		{DirRel + "/" + ArtifactDomainModel, "backend", true},
		{DirRel + "/" + ArtifactUITokens, "design", true},
		{DirRel + "/" + ArtifactNFR, OwnerOrchestrator, true},
		{DirRel + "/" + ArtifactOpenAPI, "frontend", false},
		{DirRel + "/" + ArtifactNFR, "backend", false},
		{EpochFileRel, OwnerOrchestrator, false},
		{DirRel + "/" + ArtifactOpenAPI, "", false},
	} {
		if got := OwnedBy(c.rel, c.owner); got != c.want {
			t.Errorf("OwnedBy(%s, %q) = %v", c.rel, c.owner, got)
		}
	}
}
