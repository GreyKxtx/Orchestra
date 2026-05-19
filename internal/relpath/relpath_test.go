package relpath

import (
	"testing"

	"github.com/orchestra/orchestra/internal/protocol"
)

func TestNormalize_Cases(t *testing.T) {
	tests := []struct {
		in       string
		wantPath string
		wantCode protocol.ErrorCode
	}{
		{"foo/bar.go", "foo/bar.go", ""},
		{"  foo/bar.go  ", "foo/bar.go", ""},
		{"foo\\bar.go", "foo/bar.go", ""},
		{"./foo.go", "foo.go", ""},
		{"foo/./bar.go", "foo/bar.go", ""},
		{"foo/../bar.go", "bar.go", ""},
		{"", "", protocol.InvalidLLMOutput},
		{"   ", "", protocol.InvalidLLMOutput},
		{".", "", protocol.InvalidLLMOutput},
		{"..", "", protocol.PathTraversal},
		{"../escape.go", "", protocol.PathTraversal},
		{"../../also.go", "", protocol.PathTraversal},
	}
	for _, tc := range tests {
		got, err := Normalize(tc.in)
		if tc.wantCode == "" {
			if err != nil {
				t.Errorf("Normalize(%q) unexpected err: %v", tc.in, err)
				continue
			}
			if got != tc.wantPath {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.wantPath)
			}
			continue
		}
		if err == nil {
			t.Errorf("Normalize(%q) expected error code %v, got nil (path=%q)", tc.in, tc.wantCode, got)
			continue
		}
		if err.Code != tc.wantCode {
			t.Errorf("Normalize(%q) code=%v, want %v", tc.in, err.Code, tc.wantCode)
		}
	}
}
