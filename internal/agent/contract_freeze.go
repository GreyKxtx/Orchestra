package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
)

// gateContractFreeze mirrors config.GateContractFreeze (G6) without importing
// the config package.
const gateContractFreeze = "contract_freeze"

// handleContractFreeze is the runtime side of the contract_freeze tool
// (stage 2.5, spec §3.5/§5.3): Artifact Verify (built-ins + spectral when
// exec is allowed) → G6 human gate → hash all artifacts into EPOCH.yaml →
// sync contract_epoch into state.md. Fail-closed at every step.
func (a *Agent) handleContractFreeze(ctx context.Context) ([]byte, error) {
	if a.opts.Mode != ModeOrchestra {
		return nil, fmt.Errorf("contract_freeze is available only to the Orchestra Lead")
	}
	root := a.tools.WorkspaceRoot()

	if issues := contract.VerifyArtifacts(root); len(issues) > 0 {
		return nil, fmt.Errorf("artifact_verify failed — fix the artifacts and call contract_freeze again:\n- %s",
			strings.Join(issues, "\n- "))
	}
	if a.opts.AllowExec {
		if findings := contract.SpectralLint(ctx, root); findings != "" {
			return nil, fmt.Errorf("spectral lint failed on %s:\n%s", contract.ArtifactOpenAPI, findings)
		}
	}

	if a.opts.HumanGates[gateContractFreeze] {
		if a.opts.QuestionAsker == nil {
			return nil, fmt.Errorf("human gate %s (G6) requires user confirmation but no interactive channel is available; unblock: run with question support or set orchestra.gates.%s: off",
				gateContractFreeze, gateContractFreeze)
		}
		answers, err := a.opts.QuestionAsker.Ask(ctx, []tools.QuestionItem{{
			Question: "G6: Freeze contract artifacts (Domain_Model, NFR, OpenAPI v0, UI_Tokens) into EPOCH.yaml and open parallel department work?",
			Options:  []string{"yes", "no"},
		}})
		if err != nil {
			return nil, fmt.Errorf("human gate %s: confirmation failed: %w", gateContractFreeze, err)
		}
		if len(answers) == 0 || !isAffirmativeAnswer(answers[0]) {
			return nil, fmt.Errorf("human gate %s: user declined the freeze; do not retry — resolve the user's concern first", gateContractFreeze)
		}
	}

	e, err := contract.FreezeAll(root, contract.DefaultOwners)
	if err != nil {
		return nil, err
	}

	// Mirror the epoch into state.md so the phase machine and TUI see it.
	if st, found, stErr := orchestrastate.Load(root); stErr == nil && found {
		st.ContractEpoch = e.Epoch
		_ = orchestrastate.Save(root, st)
	}
	if decisions.Adopted(root) {
		_ = decisions.Append(root, []decisions.Entry{{
			Kind:     "decision",
			Question: "contract_freeze (G6)",
			Answer:   fmt.Sprintf("contract frozen at epoch %d", e.Epoch),
		}})
	}

	resp, _ := json.Marshal(map[string]any{
		"status":     "frozen",
		"epoch":      e.Epoch,
		"epoch_file": contract.EpochFileRel,
		"artifacts":  e.Artifacts,
		"next":       "set phase: execution in .orchestra/state.md and spawn department Leads; every WorkOrder must carry contract_refs with these hashes",
	})
	return resp, nil
}
