// Package autorouter classifies a user query into build|plan|explore|ask for mode=agent.
package autorouter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/llm"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
)

// Decision is the classifier output.
type Decision struct {
	Mode       string  `json:"mode"` // build | plan | explore | ask
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// HeuristicClassify picks a mode without an LLM call (fallback / offline).
func HeuristicClassify(query string) Decision {
	q := strings.ToLower(strings.TrimSpace(query))
	switch {
	case containsAny(q, "спланируй", "план ", "plan ", "architecture", "design ", "как лучше", "roadmap"):
		return Decision{Mode: "plan", Confidence: 0.7, Reason: "heuristic: planning keywords"}
	case containsAny(q, "найди", "где ", "find ", "search ", "locate ", "кто использует", "where is", "explore ", "покажи файл"):
		return Decision{Mode: "explore", Confidence: 0.65, Reason: "heuristic: search keywords"}
	case containsAny(q, "объясни", "что делает", "how does", "explain ", "describe ", "что такое", "what is"):
		return Decision{Mode: "ask", Confidence: 0.6, Reason: "heuristic: explain/Q&A keywords"}
	default:
		return Decision{Mode: "build", Confidence: 0.5, Reason: "heuristic: default build"}
	}
}

func containsAny(q string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(q, w) {
			return true
		}
	}
	return false
}

// Classify uses a one-shot LLM JSON reply, falling back to HeuristicClassify.
func Classify(ctx context.Context, client llm.Client, query string) Decision {
	fallback := HeuristicClassify(query)
	if client == nil || strings.TrimSpace(query) == "" {
		return fallback
	}
	system := promptpkg.LoadEmbedded("auto-router.txt")
	if system == "" {
		system = defaultRouterPrompt
	}
	resp, err := client.Complete(ctx, llm.CompleteRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: query},
		},
	})
	if err != nil || resp == nil {
		return fallback
	}
	raw := strings.TrimSpace(resp.Message.Content)
	if raw == "" && len(resp.Message.Parts) > 0 {
		for _, p := range resp.Message.Parts {
			if p.Kind == llm.PartText {
				raw += p.Text
			}
		}
		raw = strings.TrimSpace(raw)
	}
	dec, ok := parseDecision(raw)
	if !ok {
		return fallback
	}
	if dec.Confidence < 0.4 {
		dec.Mode = fallback.Mode
		dec.Reason = fmt.Sprintf("low confidence (%.2f); %s", dec.Confidence, fallback.Reason)
		dec.Confidence = fallback.Confidence
	}
	return dec
}

func parseDecision(raw string) (Decision, bool) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var dec Decision
	if err := json.Unmarshal([]byte(raw), &dec); err != nil {
		return Decision{}, false
	}
	mode := strings.ToLower(strings.TrimSpace(dec.Mode))
	switch mode {
	case "build", "plan", "explore", "ask":
		dec.Mode = mode
		return dec, true
	default:
		return Decision{}, false
	}
}

const defaultRouterPrompt = `You route coding assistant turns. Reply with ONLY JSON:
{"mode":"build"|"plan"|"explore"|"ask","confidence":0.0-1.0,"reason":"..."}
- build: implement/edit/fix code
- plan: design/architecture/roadmap without editing yet
- explore: find/search/locate files and symbols
- ask: explain/describe how code works (Q&A, no edits)
No markdown.`
