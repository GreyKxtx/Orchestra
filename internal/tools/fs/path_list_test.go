package fs

import (
	"encoding/json"
	"testing"
)

// The Lead wrote `"paths": "internal/api"` and the whole grep was lost to
// "cannot unmarshal string into Go struct field" (site-sandbox run 1).
func TestSearchTextRequest_PathsAcceptsABareString(t *testing.T) {
	var req SearchTextRequest
	if err := json.Unmarshal([]byte(`{"query":"Note","paths":"internal/api","exclude_dirs":"web"}`), &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Paths) != 1 || req.Paths[0] != "internal/api" {
		t.Fatalf("paths: %v", req.Paths)
	}
	if len(req.ExcludeDirs) != 1 || req.ExcludeDirs[0] != "web" {
		t.Fatalf("exclude_dirs: %v", req.ExcludeDirs)
	}

	req = SearchTextRequest{}
	if err := json.Unmarshal([]byte(`{"query":"Note","paths":["a","b"]}`), &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Paths) != 2 || req.Paths[1] != "b" {
		t.Fatalf("array form: %v", req.Paths)
	}

	if err := json.Unmarshal([]byte(`{"query":"Note","paths":7}`), &SearchTextRequest{}); err == nil {
		t.Fatal("a number is still an error")
	}
}
