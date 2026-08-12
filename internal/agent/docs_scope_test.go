package agent

import "testing"

func TestCheckDocsEditScope(t *testing.T) {
	a := &Agent{opts: Options{Mode: ModeDocs}}

	// Reads are unrestricted.
	if err := a.checkDocsEditScope("read", []byte(`{"path":"internal/core/core.go"}`)); err != nil {
		t.Fatalf("read should pass: %v", err)
	}

	// Allowed writes: L1 conventions, MANIFEST dir, docs tree.
	for _, p := range []string{
		".orchestra/playbooks/conventions.md",
		"./.orchestra/docs/MANIFEST.md",
		"docs/architecture/overview.md",
		"docs/architecture/adr/0001-tiers.md",
		"docs/api/README.md",
	} {
		if err := a.checkDocsEditScope("write", []byte(`{"path":"`+p+`","content":"x"}`)); err != nil {
			t.Fatalf("write %s should pass: %v", p, err)
		}
	}

	// Denied: production code, PRD, contract, per-dept playbooks, runbooks.
	for _, p := range []string{
		"main.go",
		".orchestra/product/PRD.md",
		".orchestra/contract/Domain_Model.md",
		".orchestra/playbooks/frontend.md",
		".orchestra/playbooks/frontend@web.md",
		"docs/operations/runbooks/deploy.md",
		".orchestra/state.md",
	} {
		if err := a.checkDocsEditScope("edit", []byte(`{"path":"`+p+`","search":"x","replace":"y"}`)); err == nil {
			t.Fatalf("edit %s must be denied", p)
		}
	}

	// Other modes untouched.
	b := &Agent{opts: Options{Mode: ModeBuild}}
	if err := b.checkDocsEditScope("write", []byte(`{"path":"main.go","content":"x"}`)); err != nil {
		t.Fatalf("build mode must not be docs-scoped: %v", err)
	}
}
