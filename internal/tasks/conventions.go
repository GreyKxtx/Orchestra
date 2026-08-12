package tasks

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/decisions"
)

// L1 project conventions (spec §6.1): written by the Docs Lead at stage 1,
// injected into the prompt of every Lead-grade child so departments share one
// set of cross-cutting rules without a common dialog history.

// ConventionsRelPath is the L1 playbook location relative to project root.
const ConventionsRelPath = ".orchestra/playbooks/conventions.md"

// conventionsInjectMaxBytes caps the injected block; the child can read the
// full file itself when it needs more.
const conventionsInjectMaxBytes = 4096

// conventionsInjectMode reports child modes that receive the L1 block.
// Workers stay lean (their rules arrive via the WorkOrder from their Lead);
// scouts (explore/ask) are read-only; the Docs Lead owns the file itself.
func conventionsInjectMode(mode agent.Mode) bool {
	switch mode {
	case agent.ModeArchitecture, agent.ModeGeneral, agent.ModeDebug:
		return true
	}
	return false
}

// decisionsInjectMaxBytes caps the injected decision-log tail.
const decisionsInjectMaxBytes = 4096

// loadDecisionLog returns the <decision_log> block for Lead-grade children
// (spec §4.3: answers are injected into every subsequent spawn), or "" when
// the log does not exist or the mode is exempt. Same mode set as conventions:
// workers get their context via the WorkOrder from their Lead.
func loadDecisionLog(root string, mode agent.Mode) string {
	if !conventionsInjectMode(mode) {
		return ""
	}
	tail := decisions.Tail(root, decisionsInjectMaxBytes)
	if tail == "" {
		return ""
	}
	return "<decision_log source=\"" + decisions.FileRel + "\">\n" + tail + "\n</decision_log>"
}

// loadProjectConventions returns the <project_conventions> block for the
// child prompt, or "" when the playbook does not exist or the mode is exempt.
func loadProjectConventions(root string, mode agent.Mode) string {
	if !conventionsInjectMode(mode) {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ConventionsRelPath)))
	if err != nil {
		return ""
	}
	body := strings.TrimSpace(string(b))
	if body == "" {
		return ""
	}
	if len(body) > conventionsInjectMaxBytes {
		body = body[:conventionsInjectMaxBytes] + "\n...(truncated; read " + ConventionsRelPath + " for the full file)"
	}
	return "<project_conventions source=\"" + ConventionsRelPath + "\">\n" + body + "\n</project_conventions>"
}
