package playbooks

import "testing"

func TestParseLocalOverlayPath(t *testing.T) {
	dept, ok := ParseLocalOverlayPath(".orchestra/playbooks/local/frontend@web.md")
	if !ok || dept != "frontend@web" {
		t.Fatalf("got %q ok=%v", dept, ok)
	}
	if _, ok := ParseLocalOverlayPath(".orchestra/playbooks/local/nested/x.md"); ok {
		t.Fatal("nested path must be denied")
	}
	if _, ok := ParseLocalOverlayPath(".orchestra/playbooks/conventions.md"); ok {
		t.Fatal("L2 path is not local overlay")
	}
}

func TestLocalOverlayApproved(t *testing.T) {
	log := "approve vitest for frontend overlay"
	body := "---\ndecision_ref: approve vitest for frontend overlay\n---\n\n## Rules\n"
	if !LocalOverlayApproved(body, log) {
		t.Fatal("expected approved")
	}
	if LocalOverlayApproved(body, "other log") {
		t.Fatal("expected unapproved")
	}
}
