package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// HookSpec is one configured hook: a command, and optionally the tools it
// applies to.
//
// One global pre_tool command means a gate script has to re-implement the
// dispatch itself — read ORCH_TOOL_NAME, decide whether it cares, exit 0 for
// everything else — and it pays a subprocess spawn on every read and every ls
// to do it. A matcher moves that decision into the config, where it can be
// read.
type HookSpec struct {
	// Match is a regular expression tested against the tool name. Empty
	// matches every tool, which is what the single-command form has always
	// done. The expression is unanchored on purpose: a hook written for
	// "bash" should also cover a later "bash.background", because a matcher
	// that silently stops covering a tool is the dangerous failure, while one
	// that covers too much only costs a spawn.
	Match string `yaml:"match,omitempty"`
	// Command is the program and its arguments.
	Command []string `yaml:"command"`
	// TimeoutMS overrides hooks.timeout_ms for this hook alone.
	TimeoutMS int `yaml:"timeout_ms,omitempty"`
}

// HookList is a list of hooks that accepts both configured forms:
//
//	pre_tool: ["sh", "-c", "..."]          one command, every tool
//	pre_tool:
//	  - match: "bash|write"                 several hooks, each with a matcher
//	    command: ["./gate.sh"]
//
// The first form is what every config written before matchers existed uses.
// It has to keep working unchanged: an upgrade that silently drops a user's
// gate hook is worse than not shipping matchers at all.
type HookList []HookSpec

// UnmarshalYAML decides between the two forms by the kind of the first
// element: a scalar means the whole sequence is one command line.
func (l *HookList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("hooks: expected a list, got %s", nodeKindName(node.Kind))
	}
	if len(node.Content) == 0 {
		*l = nil
		return nil
	}

	if node.Content[0].Kind == yaml.ScalarNode {
		var cmd []string
		if err := node.Decode(&cmd); err != nil {
			return fmt.Errorf("hooks: %w", err)
		}
		if len(cmd) == 0 {
			*l = nil
			return nil
		}
		*l = HookList{{Command: cmd}}
		return nil
	}

	var specs []HookSpec
	if err := node.Decode(&specs); err != nil {
		return fmt.Errorf("hooks: %w", err)
	}
	out := make(HookList, 0, len(specs))
	for _, s := range specs {
		// A hook with no command would spawn nothing on every tool call and
		// report the spawn failure as a denial.
		if len(s.Command) == 0 {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		out = nil
	}
	*l = out
	return nil
}

// MarshalYAML writes the single-command form back when that is what the list
// holds. Saving settings from the TUI re-marshals the whole config; a user who
// wrote a plain command list should not find it restructured by an unrelated
// change to their editor preference.
func (l HookList) MarshalYAML() (any, error) {
	if len(l) == 1 && l[0].Match == "" && l[0].TimeoutMS == 0 {
		return l[0].Command, nil
	}
	return []HookSpec(l), nil
}

func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.MappingNode:
		return "a mapping"
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.AliasNode:
		return "an alias"
	default:
		return "an unexpected node"
	}
}
