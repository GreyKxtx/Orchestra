package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/contract"
)

// WorkOrder is the Lead → Worker JSON contract (docs/architecture/planner-worker.md).
type WorkOrder struct {
	TaskID              string         `json:"task_id,omitempty"`
	Tier                string         `json:"tier,omitempty"`
	TargetFile          string         `json:"target_file,omitempty"`
	TargetFiles         []string       `json:"target_files,omitempty"`
	TargetSymbol        string         `json:"target_symbol,omitempty"`
	Intent              string         `json:"intent,omitempty"`
	Context             map[string]any `json:"context,omitempty"`
	Instructions        []string       `json:"instructions,omitempty"`
	Constraints         []string       `json:"constraints,omitempty"`
	ReadonlyReferences  []string       `json:"readonly_references,omitempty"`
	AllowedSymbols      []string       `json:"allowed_symbols,omitempty"`
	AcceptanceCriteria  []string       `json:"acceptance_criteria,omitempty"`

	// ContractRefs pin the WorkOrder to contract artifact versions
	// (spec §5.3): each ref carries the sha256 the Lead read. The runtime
	// verifies them against EPOCH.yaml at spawn and again on success.
	ContractRefs []contract.Ref `json:"contract_refs,omitempty"`

	// AcceptanceChecks are machine-executable acceptance criteria
	// (spec §5.4, ADR-4). Executed by the runtime, never self-assessed.
	AcceptanceChecks []AcceptanceCheck `json:"acceptance_checks,omitempty"`

	// DependsOn names the WorkOrders (by task_id) or tasks that must finish
	// successfully before this one starts. The runtime holds the worker
	// until then and hands it their results; a failed dependency fails it
	// with blocked_reason dependency_unmet instead of running it blind.
	DependsOn []string `json:"depends_on,omitempty"`
}

// AcceptanceCheck is one runnable acceptance probe. Execution follows the
// exec.run consent policy (--allow-exec / exec.confirm: false).
type AcceptanceCheck struct {
	Cmd          string `json:"cmd"`
	ExpectExit   int    `json:"expect_exit,omitempty"`
	ExpectStdout string `json:"expect_stdout,omitempty"`
}

// UnmarshalJSON accepts the object form and a bare command string. The Lead
// prompt lists acceptance_checks[] without a shape, and a 27B Lead wrote
// `["go build ./...", "go test ./..."]` — two whole WorkOrders were refused
// with "cannot unmarshal string into Go struct field" for it (2026-09-18). A
// string is a command expected to exit 0.
func (c *AcceptanceCheck) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var cmd string
		if err := json.Unmarshal(b, &cmd); err != nil {
			return err
		}
		*c = AcceptanceCheck{Cmd: cmd}
		return nil
	}
	type plain AcceptanceCheck
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*c = AcceptanceCheck(p)
	return nil
}

// ValidateWorkOrder checks a parsed WorkOrder; returns error when intent is missing.
func ValidateWorkOrder(wo *WorkOrder) error {
	if wo == nil {
		return fmt.Errorf("workorder: nil")
	}
	if strings.TrimSpace(wo.Intent) == "" && len(wo.Instructions) == 0 {
		return fmt.Errorf("workorder: intent or instructions[] required")
	}
	return nil
}

// EditScopePaths returns normalized paths the worker may edit/write.
// target_files[] wins over target_file; empty slice = no runtime restriction.
func EditScopePaths(wo *WorkOrder) []string {
	if wo == nil {
		return nil
	}
	if len(wo.TargetFiles) > 0 {
		out := make([]string, 0, len(wo.TargetFiles))
		for _, p := range wo.TargetFiles {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if s := strings.TrimSpace(wo.TargetFile); s != "" {
		return []string{s}
	}
	return nil
}

// ParseWorkOrderJSON parses and validates JSON goal text for worker children.
func ParseWorkOrderJSON(raw string) (*WorkOrder, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("workorder: goal must be JSON for subagent_type=worker")
	}
	var wo WorkOrder
	if err := json.Unmarshal([]byte(raw), &wo); err != nil {
		return nil, fmt.Errorf("workorder: invalid JSON: %w", err)
	}
	if err := ValidateWorkOrder(&wo); err != nil {
		return nil, err
	}
	return &wo, nil
}
