package plan_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/plan"
)

func TestIsOrchestraLeadWritablePath(t *testing.T) {
	if !plan.IsOrchestraLeadWritablePath(".orchestra/state.md", "") {
		t.Fatal("state.md should be writable")
	}
	if !plan.IsOrchestraLeadWritablePath(".orchestra/depts/frontend@web.md", "") {
		t.Fatal("dept scratchpads must be Lead-writable")
	}
	if plan.IsOrchestraLeadWritablePath(".orchestra/depts/a/b.md", "") {
		t.Fatal("nested paths under depts must be denied")
	}
	if plan.IsOrchestraLeadWritablePath(".orchestra/depts/../secrets.md", "") {
		t.Fatal("traversal out of depts must be denied")
	}
	if plan.IsOrchestraLeadWritablePath(".orchestra/depts/x.txt", "") {
		t.Fatal("non-md files under depts must be denied")
	}
	if !plan.IsOrchestraLeadWritablePath(".orchestra/plans/foo.md", "") {
		t.Fatal("plans should be writable")
	}
	if plan.IsOrchestraLeadWritablePath("internal/foo.go", "") {
		t.Fatal("production code must be denied")
	}
}
