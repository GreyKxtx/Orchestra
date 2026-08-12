package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
)

// Human gates G2/G3 (spec §4.4): a required gate confirms the mutating git
// action with the user via QuestionAsker before the tool executes. G1 (PRD)
// lives in the phase guard; G4 (deploy) rides on the exec consent gate.

// gateForTool maps a tool name to its gate key and confirmation question.
func gateForTool(name string, input json.RawMessage) (key, question string) {
	switch name {
	case "git.commit":
		var p struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(input, &p)
		q := "Gate G2 · Создать commit?"
		if m := strings.TrimSpace(p.Message); m != "" {
			q = fmt.Sprintf("Gate G2 · Создать commit: %q?", firstLine(m))
		}
		return "git_commit", q
	case "git.push":
		var p struct {
			Remote string `json:"remote"`
			Branch string `json:"branch"`
			Force  bool   `json:"force"`
		}
		_ = json.Unmarshal(input, &p)
		remote := p.Remote
		if remote == "" {
			remote = "origin"
		}
		target := remote
		if p.Branch != "" {
			target += "/" + p.Branch
		}
		q := fmt.Sprintf("Gate G3 · Push в %s?", target)
		if p.Force {
			q = fmt.Sprintf("Gate G3 · FORCE push (--force-with-lease) в %s?", target)
		}
		return "git_push", q
	}
	return "", ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// confirmHumanGate blocks a gated tool until the user confirms. Fail-closed:
// a required gate without an interactive channel denies the call with an
// explicit unblock path (spec §5.2).
func (a *Agent) confirmHumanGate(ctx context.Context, name string, input json.RawMessage) error {
	if a == nil || len(a.opts.HumanGates) == 0 {
		return nil
	}
	key, question := gateForTool(name, input)
	if key == "" || !a.opts.HumanGates[key] {
		return nil
	}
	if a.opts.QuestionAsker == nil {
		return fmt.Errorf(
			"human gate %s: %s requires user confirmation but no interactive channel is available; unblock: run in TUI/CLI with question support or set orchestra.gates.%s: off",
			key, name, key,
		)
	}
	answers, err := a.opts.QuestionAsker.Ask(ctx, []tools.QuestionItem{{
		Question: question,
		Options:  []string{"yes", "no"},
	}})
	if err != nil {
		return fmt.Errorf("human gate %s: confirmation failed: %w", key, err)
	}
	if len(answers) == 0 || !isAffirmativeAnswer(answers[0]) {
		return fmt.Errorf("human gate %s: user declined %s; do not retry — continue without it or ask the user what to do next", key, name)
	}
	return nil
}

// isAffirmativeAnswer treats an explicit yes (or option #1) as approval;
// anything else — including empty — is a decline (fail-closed).
func isAffirmativeAnswer(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "y", "да", "1", "ok", "approve", "approved":
		return true
	}
	return false
}
