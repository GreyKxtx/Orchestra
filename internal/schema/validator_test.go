package schema

import (
	"testing"

	"github.com/orchestra/orchestra/internal/externalpatch"
)

func TestValidator_ExternalPatches_Valid(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	raw := `{
  "patches": [
    {
      "type": "file.search_replace",
      "path": "a.go",
      "search": "old",
      "replace": "new",
      "file_hash": "sha256:abc"
    }
  ]
}`

	var ps externalpatch.PatchSet
	if coreErr := v.ValidateAndDecode(KindExternalPatches, raw, &ps); coreErr != nil {
		t.Fatalf("ValidateAndDecode failed: %v", coreErr)
	}
	if len(ps.Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(ps.Patches))
	}
}

func TestValidator_ExternalPatches_InvalidSchema(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	raw := `{"patches":[{"type":"file.search_replace","path":"a.go","search":"x","replace":"y"}]}`
	var ps externalpatch.PatchSet
	coreErr := v.ValidateAndDecode(KindExternalPatches, raw, &ps)
	if coreErr == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestValidator_InvalidJSON(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	raw := `{"patches":[`
	var ps externalpatch.PatchSet
	coreErr := v.ValidateAndDecode(KindExternalPatches, raw, &ps)
	if coreErr == nil {
		t.Fatalf("expected error, got nil")
	}
}
