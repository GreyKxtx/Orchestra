package plan_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/plan"
)

func TestIsOrchestraLeadWritablePath(t *testing.T) {
	if !plan.IsOrchestraLeadWritablePath(".orchestra/state.md", "") {
		t.Fatal("state.md should be writable")
	}
	if !plan.IsOrchestraLeadWritablePath(".orchestra/plans/foo.md", "") {
		t.Fatal("plans should be writable")
	}
	if plan.IsOrchestraLeadWritablePath("internal/foo.go", "") {
		t.Fatal("production code must be denied")
	}
}
