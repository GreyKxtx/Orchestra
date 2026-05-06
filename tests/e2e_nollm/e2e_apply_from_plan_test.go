package e2e_nollm

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
)

func TestApply_FromPlan_DryRun_And_Apply(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0755); err != nil {
		t.Fatal(err)
	}

	// Config: JSON is valid YAML too.
	cfg := map[string]any{
		"project_root":     root,
		"exclude_dirs":     []string{".git", ".orchestra"},
		"context_limit_kb": 50,
		"llm": map[string]any{
			"api_base":    "http://localhost:8000/v1",
			"api_key":     "test",
			"model":       "test-model",
			"max_tokens":  1024,
			"temperature": 0.0,
			"timeout_s":   300,
		},
	}
	bCfg, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(root, ".orchestra.yml"), bCfg, 0644); err != nil {
		t.Fatal(err)
	}

	orig := []byte("hello old world\n")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), orig, 0644); err != nil {
		t.Fatal(err)
	}

	// Build a deterministic plan.json with ops (no LLM required).
	h := cache.ComputeSHA256(orig)
	plan := map[string]any{
		"protocol_version":  protocol.ProtocolVersion,
		"ops_version":       protocol.OpsVersion,
		"tools_version":     protocol.ToolsVersion,
		"query":             "from-plan test",
		"generated_at_unix": 1,
		"ops": []any{
			map[string]any{
				"op":      "file.write_atomic",
				"path":    "a.txt",
				"content": "hello new world\n",
				"conditions": map[string]any{
					"file_hash": h,
				},
			},
		},
	}
	planPath := filepath.Join(root, "plan_input.json")
	bPlan, _ := json.MarshalIndent(plan, "", "  ")
	if err := os.WriteFile(planPath, bPlan, 0644); err != nil {
		t.Fatal(err)
	}

	bin := buildOrchestraOnce(t)

	// --- Dry-run via --plan-only ---
	{
		cmd := exec.Command(bin, "apply", "--from-plan", planPath, "--plan-only")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("apply --plan-only failed: %v\n%s", err, string(out))
		}

		after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
		if !bytes.Equal(after, orig) {
			t.Fatalf("dry-run must not modify file")
		}
		// Artifacts should be created.
		if _, err := os.Stat(filepath.Join(root, ".orchestra", "plan.json")); err != nil {
			t.Fatalf("expected plan.json artifact: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, ".orchestra", "diff.txt")); err != nil {
			t.Fatalf("expected diff.txt artifact: %v", err)
		}
	}

	// --- Apply via --from-plan + --apply ---
	{
		cmd := exec.Command(bin, "apply", "--from-plan", planPath, "--apply")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("apply --apply failed: %v\n%s", err, string(out))
		}

		after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
		if string(after) != "hello new world\n" {
			t.Fatalf("unexpected applied content: %q", string(after))
		}
		// Backup should exist and match original.
		bak, err := os.ReadFile(filepath.Join(root, "a.txt.orchestra.bak"))
		if err != nil {
			t.Fatalf("expected backup file: %v", err)
		}
		if !bytes.Equal(bak, orig) {
			t.Fatalf("backup content mismatch")
		}
	}
}
