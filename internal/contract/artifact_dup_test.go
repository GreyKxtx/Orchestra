package contract

import (
	"strings"
	"testing"
)

func TestCheckDomainDuplicates(t *testing.T) {
	ok := "# Domain\n\n## Order\nfields\n\n## Payment\nfields\n"
	if msg := checkDomainDuplicates([]byte(ok)); msg != "" {
		t.Fatalf("unique entities must pass: %s", msg)
	}
	dup := "# Domain\n\n## Order\nfields\n\n## order\nother fields\n"
	msg := checkDomainDuplicates([]byte(dup))
	if msg == "" || !strings.Contains(msg, "duplicate entity") {
		t.Fatalf("case-insensitive duplicate must fail: %q", msg)
	}
	// Wired into checkArtifact for Domain_Model.md.
	if msg := checkArtifact(ArtifactDomainModel, []byte(dup)); !strings.Contains(msg, "duplicate entity") {
		t.Fatalf("checkArtifact must run the dup check: %q", msg)
	}
}

func TestDefaultOwnersCoverRequiredArtifacts(t *testing.T) {
	for _, name := range RequiredArtifacts {
		if DefaultOwners[name] == "" {
			t.Fatalf("artifact %s has no default owner", name)
		}
	}
}
