package ckg

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// FormatNodesForPrompt renders nodes as a compact <ckg_context> block.
// Returns an empty string if nodes is empty.
// The total output (including XML tags) is capped at maxBytes.
func FormatNodesForPrompt(nodes []Node, maxBytes int) string {
	if len(nodes) == 0 {
		return ""
	}
	const header = "<ckg_context>\n"
	const footer = "</ckg_context>"
	if maxBytes <= len(header)+len(footer) {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(header)
	for _, n := range nodes {
		line := fmt.Sprintf("%s (%s, L%d-%d)\n", n.FQN, n.Kind, n.LineStart, n.LineEnd)
		if sb.Len()+len(line)+len(footer) > maxBytes {
			break
		}
		sb.WriteString(line)
	}
	sb.WriteString(footer)
	return sb.String()
}

const defaultPromptContextTokens = 1500
const maxPromptSeedNodes = 6

// FormatPromptContext is the step-1 user-prompt injection: ranked FQNs plus a
// depth-1 neighborhood for each seed. maxTokens 0 → 1500 (~6KB at 4 bytes/token).
func (s *Store) FormatPromptContext(ctx context.Context, nodes []Node, maxTokens int) string {
	if s == nil || len(nodes) == 0 {
		return ""
	}
	if maxTokens <= 0 {
		maxTokens = defaultPromptContextTokens
	}
	maxBytes := maxTokens * bytesPerToken
	const header = "<ckg_context>\n"
	const footer = "</ckg_context>"
	if maxBytes <= len(header)+len(footer) {
		return ""
	}

	limit := len(nodes)
	if limit > maxPromptSeedNodes {
		limit = maxPromptSeedNodes
	}

	var sb strings.Builder
	sb.WriteString(header)
	for i := 0; i < limit; i++ {
		n := nodes[i]
		line := fmt.Sprintf("%s (%s, L%d-%d)\n", n.FQN, n.Kind, n.LineStart, n.LineEnd)
		if sb.Len()+len(line)+len(footer) > maxBytes {
			break
		}
		sb.WriteString(line)
		remain := maxBytes - sb.Len() - len(footer)
		if remain < 80 || n.FQN == "" {
			continue
		}
		sg, err := s.TraverseBFS(ctx, n.FQN, DirectionBoth, TraversalOptions{
			MaxDepth:     1,
			MaxNodes:     16,
			IncludeTypes: true,
		})
		if err != nil || sg == nil || len(sg.Nodes) <= 1 && len(sg.Edges) == 0 {
			continue
		}
		tree := FormatSubgraphContext(sg, remain/bytesPerToken)
		if tree == "" || sb.Len()+len(tree)+1+len(footer) > maxBytes {
			continue
		}
		sb.WriteString(tree)
		sb.WriteByte('\n')
	}
	sb.WriteString(footer)
	return sb.String()
}

const defaultSubgraphTokens = 800
const bytesPerToken = 4

// FormatSubgraphContext renders a BFS neighborhood as an indented tree for the LLM.
// maxTokens is an approximate prompt budget (0 → 800). Distant leaves are dropped
// with "... (+N more nodes)" when the budget is exhausted.
//
// Cycle edges (A→B→C→A) are not expanded a second time; the back-edge is marked
// `(cycle)` so the printer cannot recurse forever.
func FormatSubgraphContext(sg *Subgraph, maxTokens int) string {
	if sg == nil || sg.Root == nil {
		return ""
	}
	if maxTokens <= 0 {
		maxTokens = defaultSubgraphTokens
	}
	maxBytes := maxTokens * bytesPerToken

	rootFQN := sg.Root.FQN
	maxDepth := 0
	for _, d := range sg.Depth {
		if d > maxDepth {
			maxDepth = d
		}
	}

	header := fmt.Sprintf("<ckg_subgraph root=%q depth=\"%d\">\n", shortLabel(sg.Root), maxDepth)
	footer := "</ckg_subgraph>"
	budget := maxBytes - len(header) - len(footer)
	if budget < 64 {
		return header + footer
	}

	callers := childrenOf(sg, rootFQN, true)
	callees := childrenOf(sg, rootFQN, false)
	hidden := 0

	var tmp strings.Builder
	if len(callers) > 0 {
		branch := "├── "
		indent := "  │   "
		if len(callees) == 0 {
			branch = "└── "
			indent = "      "
		}
		tmp.WriteString("  " + branch + "callers:\n")
		renderKids(&tmp, sg, callers, indent, map[string]bool{rootFQN: true}, &hidden, budget)
	}
	if len(callees) > 0 {
		tmp.WriteString("  └── callees:\n")
		renderKids(&tmp, sg, callees, "      ", map[string]bool{rootFQN: true}, &hidden, budget)
	}
	if hidden > 0 {
		fmt.Fprintf(&tmp, "  ... (+%d more nodes)\n", hidden)
	}
	out := tmp.String()
	if len(header)+len(out)+len(footer) > maxBytes {
		keep := maxBytes - len(header) - len(footer) - 32
		if keep < 0 {
			keep = 0
		}
		if keep > len(out) {
			keep = len(out)
		}
		out = out[:keep] + "\n  ... (+truncated)\n"
	}
	return header + out + footer
}

type treeChild struct {
	fqn string
	rel string
}

func childrenOf(sg *Subgraph, parent string, upstream bool) []treeChild {
	var out []treeChild
	seen := map[string]bool{}
	for _, e := range sg.Edges {
		var child string
		if upstream {
			if e.TargetFQN != parent {
				continue
			}
			child = e.SourceFQN
		} else {
			if e.SourceFQN != parent {
				continue
			}
			child = e.TargetFQN
		}
		if child == "" || child == parent || seen[child] {
			continue
		}
		seen[child] = true
		out = append(out, treeChild{fqn: child, rel: e.Relation})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].fqn < out[j].fqn })
	return out
}

func renderKids(sb *strings.Builder, sg *Subgraph, kids []treeChild, indent string, visiting map[string]bool, hidden *int, budget int) {
	for i, k := range kids {
		if sb.Len() > budget {
			*hidden += len(kids) - i
			return
		}
		n := sg.Nodes[k.fqn]
		last := i == len(kids)-1
		branch := "├── "
		nextIndent := indent + "│   "
		if last {
			branch = "└── "
			nextIndent = indent + "    "
		}
		label := k.fqn
		loc := ""
		if n != nil {
			label = shortLabel(n)
			if n.RelPath != "" && n.LineStart > 0 {
				loc = fmt.Sprintf(" (%s:%d)", n.RelPath, n.LineStart)
			}
		}
		if visiting[k.fqn] {
			fmt.Fprintf(sb, "%s%s%s (cycle)\n", indent, branch, label)
			continue
		}
		if k.rel == "instantiates" {
			fmt.Fprintf(sb, "%s%s[instantiates] -> %s%s\n", indent, branch, label, loc)
		} else {
			fmt.Fprintf(sb, "%s%s%s%s\n", indent, branch, label, loc)
		}
		if n != nil && n.Kind == "external" {
			continue
		}
		grand := childrenOf(sg, k.fqn, false)
		if len(grand) == 0 {
			continue
		}
		visiting[k.fqn] = true
		renderKids(sb, sg, grand, nextIndent, visiting, hidden, budget)
		delete(visiting, k.fqn)
	}
}

func shortLabel(n *Node) string {
	if n == nil {
		return ""
	}
	if n.ShortName != "" {
		if n.Package != "" && !strings.Contains(n.ShortName, ".") {
			return n.Package + "." + n.ShortName
		}
		return n.ShortName
	}
	return n.FQN
}
