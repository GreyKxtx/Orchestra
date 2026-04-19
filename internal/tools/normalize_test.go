package tools

import (
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/ops"
)

func TestNormalizeOpsJSON_TypeToOp(t *testing.T) {
	// Test normalization of "type" → "op" in ops
	input := json.RawMessage(`{
		"ops": [
			{
				"type": "file.replace_range",
				"path": "test.go",
				"range": {"start": {"line": 0, "col": 0}, "end": {"line": 1, "col": 0}},
				"expected": "old",
				"replacement": "new"
			}
		],
		"dry_run": true
	}`)

	normalized := normalizeOpsJSON(input)

	var req FSApplyOpsRequest
	if err := json.Unmarshal(normalized, &req); err != nil {
		t.Fatalf("Failed to decode normalized JSON: %v", err)
	}

	if len(req.Ops) != 1 {
		t.Fatalf("Expected 1 op, got %d", len(req.Ops))
	}

	if req.Ops[0].Op != ops.OpFileReplaceRange {
		t.Errorf("Expected op=%q, got %q", ops.OpFileReplaceRange, req.Ops[0].Op)
	}

	if req.Ops[0].Path != "test.go" {
		t.Errorf("Expected path=test.go, got %q", req.Ops[0].Path)
	}
}

func TestNormalizeOpsJSON_OpAlreadyPresent(t *testing.T) {
	// Test that normalization doesn't break when "op" is already present
	input := json.RawMessage(`{
		"ops": [
			{
				"op": "file.replace_range",
				"path": "test.go",
				"range": {"start": {"line": 0, "col": 0}, "end": {"line": 1, "col": 0}},
				"expected": "old",
				"replacement": "new"
			}
		]
	}`)

	normalized := normalizeOpsJSON(input)

	var req FSApplyOpsRequest
	if err := json.Unmarshal(normalized, &req); err != nil {
		t.Fatalf("Failed to decode normalized JSON: %v", err)
	}

	if len(req.Ops) != 1 {
		t.Fatalf("Expected 1 op, got %d", len(req.Ops))
	}

	if req.Ops[0].Op != ops.OpFileReplaceRange {
		t.Errorf("Expected op=%q, got %q", ops.OpFileReplaceRange, req.Ops[0].Op)
	}
}

func TestNormalizeOpsJSON_NoOps(t *testing.T) {
	// Test that normalization doesn't break non-ops input
	input := json.RawMessage(`{"dry_run": true}`)

	normalized := normalizeOpsJSON(input)

	// Should return original (no normalization needed)
	if string(normalized) != string(input) {
		t.Errorf("Expected no change for non-ops input")
	}
}
