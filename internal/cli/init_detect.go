package cli

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// maxReportedModels caps the "other models" list so a server with fifty pulled
// models does not bury the line that matters.
const maxReportedModels = 4

// applyDetectedLocalServer prefills api_base and model from a local inference
// server that is actually running, and returns the line to print about it.
//
// `orchestra init` wrote http://localhost:1234/v1 + qwen2.5-coder-7b
// unconditionally, which is right for LM Studio with that model pulled and
// wrong for everyone else — and wrong silently, because nothing in the output
// said the values were guesses.
//
// It returns "" and touches nothing when there was no usable detection: a
// probe that found nothing is not a reason to rewrite the defaults it never
// looked at, and an api_base with no model is worse than the pair it replaces.
func applyDetectedLocalServer(cfg *config.ProjectConfig, srv llm.LocalServer, ok bool) string {
	if !ok || cfg == nil || srv.APIBase == "" || len(srv.Models) == 0 {
		return ""
	}
	chosen := strings.TrimSpace(srv.Models[0].ID)
	if chosen == "" {
		return ""
	}
	cfg.LLM.APIBase = srv.APIBase
	cfg.LLM.Model = chosen

	report := fmt.Sprintf("Найден локальный сервер %s — api_base и model взяты из него: %s",
		srv.APIBase, chosen)

	// The pick among several is arbitrary — a local server lists everything it
	// has pulled, not what is loaded — so the alternatives have to be visible
	// or the user will not know a choice was made for them.
	if len(srv.Models) > 1 {
		others := make([]string, 0, maxReportedModels)
		for _, m := range srv.Models[1:] {
			if id := strings.TrimSpace(m.ID); id != "" {
				others = append(others, id)
			}
			if len(others) == maxReportedModels {
				break
			}
		}
		if len(others) > 0 {
			rest := ""
			if len(srv.Models)-1 > len(others) {
				rest = fmt.Sprintf(" и ещё %d", len(srv.Models)-1-len(others))
			}
			report += fmt.Sprintf("\n  Там же доступны: %s%s — поменяй llm.model в .orchestra.yml, если нужна другая",
				strings.Join(others, ", "), rest)
		}
	}
	return report
}
