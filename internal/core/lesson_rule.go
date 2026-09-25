package core

import (
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

// RuleSuggestionPayload mirrors agent.RuleSuggestion on the wire.
type RuleSuggestionPayload = wire.RuleSuggestion

// ruleSuggestionPayload converts a turn's RuleSuggestion (or nil) into its
// wire form.
func ruleSuggestionPayload(s *agent.RuleSuggestion) *RuleSuggestionPayload {
	if s == nil {
		return nil
	}
	return &RuleSuggestionPayload{
		Dept: s.Dept, File: s.File, Count: s.Count,
		Verify: s.Verify, RuleLine: s.RuleLine, Text: s.Text,
	}
}

// --- lesson.rule_respond ---

// RuleSuggestionRespond is the human's answer to a RuleSuggestion: accept
// appends RuleLine to the project's instructions file (whichever file
// actually backs the orchestra layer today — memory.FindProjectInstructions,
// the same fallback lookup A2 already uses — defaulting to ORCHESTRA.md so a
// project with none yet gets one); decline does nothing to any file. Either
// way the (dept, file, verify) signal is cleared so the same combination
// needs three fresh occurrences before asking again.
func (c *Core) RuleSuggestionRespond(params RuleSuggestionRespondParams) (*RuleSuggestionRespondResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	root := c.workspaceRoot
	key := lessons.FileAntiPatternKey(params.File, params.Verify, "")
	lessons.ClearRuleSignal(root, params.Dept, key)

	if !params.Accept {
		return &RuleSuggestionRespondResult{Applied: false}, nil
	}
	if err := appendProjectInstructionsLine(root, params.RuleLine); err != nil {
		return nil, err
	}
	return &RuleSuggestionRespondResult{Applied: true}, nil
}

// appendProjectInstructionsLine appends line as a new bullet to whichever
// file backs the orchestra layer, creating ORCHESTRA.md if none exists yet.
func appendProjectInstructionsLine(root, line string) error {
	_, name := memory.FindProjectInstructions(root)
	if name == "" {
		name = "ORCHESTRA.md"
	}
	f, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n- " + line + "\n")
	return err
}

// The wire types of this file live in protocol/wire (ARCH-4); the aliases
// keep the package's names.
type (
	RuleSuggestionRespondParams = wire.RuleSuggestionRespondParams
	RuleSuggestionRespondResult = wire.RuleSuggestionRespondResult
)
