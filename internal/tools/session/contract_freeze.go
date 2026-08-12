package session

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

// ToolContractFreeze is the stage-2.5 freeze trigger (spec §3.5, §5.3):
// the runtime — not the model — verifies the four contract artifacts
// (Artifact Verify), asks the user for G6 approval when configured, and
// hashes them into .orchestra/contract/EPOCH.yaml. Orchestra Lead only.
func ToolContractFreeze() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name: "contract_freeze",
			Description: "Freeze the contract artifacts (Domain_Model.md, NFR.md, OpenAPI.v0.yaml, UI_Tokens.skeleton.json) into .orchestra/contract/EPOCH.yaml. " +
				"Runs Artifact Verify first and fails with the issue list if any artifact is missing or degenerate. " +
				"Requires user approval (G6) when orchestra.gates.contract_freeze is required. " +
				"Call once at stage 2.5 after all four artifacts are written; call again after an accepted contract_change_request is applied.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
		Mutating: true,
	}
}
