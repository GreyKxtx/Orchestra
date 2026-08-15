package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

func exploreNavTool(name string) bool {
	switch normalizeToolName(name) {
	case "read", "grep", "glob", "ls", "explore", "symbols", "semantic_search", "repo_map",
		"lsp.definition", "lsp.references", "lsp.hover", "lsp.diagnostics", "diff.preview":
		return true
	default:
		return false
	}
}

func exploreFirstMode(mode Mode) bool {
	return mode == ModeWorker || mode == ModeOrchestra || mode == ModeArchitecture
}

func (a *Agent) resetExploreFirstGate() {
	a.exploreFirstSatisfied = false
}

func (a *Agent) markExploreFirstSatisfied(name string) {
	if a == nil || !exploreFirstMode(a.opts.Mode) {
		return
	}
	if exploreNavTool(name) {
		a.exploreFirstSatisfied = true
	}
}

// checkExploreFirstGate blocks edit/write until the agent explored the codebase.
// Workers may also satisfy by reading all WorkOrder target_files.
func (a *Agent) checkExploreFirstGate(name string, history []llm.Message) error {
	if a == nil || !exploreFirstMode(a.opts.Mode) {
		return nil
	}
	if name != "write" && name != "edit" {
		return nil
	}
	if a.exploreFirstSatisfied {
		return nil
	}
	if a.opts.Mode == ModeWorker && a.workerEditPathsRead(history) {
		return nil
	}
	switch a.opts.Mode {
	case ModeOrchestra:
		return fmt.Errorf(
			"explore-first gate: call read, grep, or explore on the repository before write/edit; " +
				"Orchestra Lead must gather evidence before mutating plans or scratchpads",
		)
	case ModeArchitecture:
		return fmt.Errorf(
			"explore-first gate: call read, grep, or explore on the repository before write/edit; " +
				"Dept Lead must read relevant code and specs before writing Brief/ТЗ or playbooks",
		)
	default:
		return fmt.Errorf(
			"explore-first gate: call read, grep, or explore on the WorkOrder scope before edit/write; " +
				"target_files must be read at least once",
		)
	}
}

func (a *Agent) workerEditPathsRead(history []llm.Message) bool {
	paths := a.opts.WorkerEditPaths
	if len(paths) == 0 {
		return false
	}
	read := workerReadPaths(history)
	for _, want := range paths {
		want = normalizeExplorePath(want)
		if want == "" {
			continue
		}
		if !read[want] {
			return false
		}
	}
	return true
}

func workerReadPaths(history []llm.Message) map[string]bool {
	out := map[string]bool{}
	for _, m := range history {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			name := normalizeToolName(tc.Function.Name)
			if name != "read" {
				continue
			}
			var req struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(tc.Function.Arguments), &req) != nil {
				continue
			}
			p := normalizeExplorePath(req.Path)
			if p != "" {
				out[p] = true
			}
		}
	}
	return out
}

func normalizeExplorePath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "./")
	return p
}

// Legacy aliases used by tests.
func (a *Agent) resetWorkerExploreGate() { a.resetExploreFirstGate() }

func (a *Agent) markWorkerExploreSatisfied(name string) { a.markExploreFirstSatisfied(name) }

func (a *Agent) checkWorkerExploreFirstGate(name string, history []llm.Message) error {
	return a.checkExploreFirstGate(name, history)
}
