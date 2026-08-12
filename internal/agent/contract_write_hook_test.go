package agent

import "testing"

func TestContractArtifactFileName(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{".orchestra/contract/Domain_Model.md", "Domain_Model.md", true},
		{"./.orchestra/contract/NFR.md", "NFR.md", true},
		{".orchestra/contract/EPOCH.yaml", "", false}, // runtime-owned, never a hook trigger
		{".orchestra/contract/sub/dir.md", "", false}, // nested paths are not artifacts
		{".orchestra/state.md", "", false},
		{"docs/contract/NFR.md", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := contractArtifactFileName(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("contractArtifactFileName(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}
