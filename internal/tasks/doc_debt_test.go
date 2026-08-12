package tasks

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/orchestrastate"
)

const manifestFixture = `# MANIFEST

| Path | Owner dept | Update trigger | Status |
|------|------------|----------------|--------|
| ` + "`docs/api/README.md`" + ` | Backend | OpenAPI change (` + "`.orchestra/contract/OpenAPI*`" + `, ` + "`api/**`" + `) | draft |
| ` + "`docs/development/setup.md`" + ` | Documentation | CI change (` + "`.github/workflows/**`" + `) | draft |
| ` + "`docs/README.md`" + ` | Documentation | free-text only, no globs | draft |
`

func TestGlobToRegexp(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"api/**", "api/v1/users.go", true},
		{"api/**", "internal/api.go", false},
		{".orchestra/contract/OpenAPI*", ".orchestra/contract/OpenAPI.v0.yaml", true},
		{".orchestra/contract/OpenAPI*", ".orchestra/contract/sub/OpenAPI.yaml", false},
		{".github/workflows/**", ".github/workflows/ci.yml", true},
		{"Dockerfile", "Dockerfile", true},
		{"Dockerfile", "sub/Dockerfile", false},
	}
	for _, c := range cases {
		re := globToRegexp(c.glob)
		if re == nil {
			t.Fatalf("globToRegexp(%q) = nil", c.glob)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Errorf("glob %q vs %q = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestRecordDocDebt(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, ".orchestra/state.md", "---\norchestra:\n  phase: execution\n  prd_status: approved\n---\n")
	writeFileT(t, root, ".orchestra/docs/MANIFEST.md", manifestFixture)

	// Edit matching a glob → debt for the mapped doc.
	got := recordDocDebt(root, []string{"api/v1/handlers.go", "main.go"})
	if len(got) != 1 || got[0] != "docs/api/README.md" {
		t.Fatalf("recorded = %v, want [docs/api/README.md]", got)
	}
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		t.Fatalf("state: %v", err)
	}
	if len(st.DocDebt) != 1 || st.DocDebt[0] != "docs/api/README.md" {
		t.Fatalf("doc_debt = %v", st.DocDebt)
	}

	// Doc updated in the same batch → no debt for it.
	if got := recordDocDebt(root, []string{".github/workflows/ci.yml", "docs/development/setup.md"}); len(got) != 0 {
		t.Fatalf("self-updating doc must not create debt, got %v", got)
	}

	// Rows without globs and non-matching edits are ignored.
	if got := recordDocDebt(root, []string{"internal/core/core.go"}); len(got) != 0 {
		t.Fatalf("no match expected, got %v", got)
	}

	// No MANIFEST → no-op.
	root2 := t.TempDir()
	if got := recordDocDebt(root2, []string{"api/x.go"}); got != nil {
		t.Fatalf("missing MANIFEST must no-op, got %v", got)
	}
}

func TestLoadDocsManifest_SkipsHeaderRows(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, ".orchestra/docs/MANIFEST.md", manifestFixture)
	rows := loadDocsManifest(root)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (glob-carrying only)", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r.docPath, "|") {
			t.Fatalf("bad docPath parse: %q", r.docPath)
		}
	}
}
