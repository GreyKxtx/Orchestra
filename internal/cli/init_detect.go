package cli

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// maxReportedModels caps the models list so a server with fifty pulled
// models does not bury the line that matters.
const maxReportedModels = 4

// applyDetectedLocalServer points api_base at a local inference server that
// is actually running, and returns the line to print about it.
//
// It never picks the model. `orchestra init` used to write
// http://localhost:1234/v1 + qwen2.5-coder-7b unconditionally, and later the
// first model the server listed — a choice made for the user, and the one a
// project went on asking every provider for. The model stays unset until the
// user picks one; the server's models are what the picker offers.
//
// It returns "" and touches nothing when there was no usable detection.
func applyDetectedLocalServer(cfg *config.ProjectConfig, srv llm.LocalServer, ok bool) string {
	if !ok || cfg == nil || srv.APIBase == "" || len(srv.Models) == 0 {
		return ""
	}
	cfg.LLM.APIBase = srv.APIBase

	ids := make([]string, 0, maxReportedModels)
	for _, m := range srv.Models {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
		if len(ids) == maxReportedModels {
			break
		}
	}
	rest := ""
	if len(srv.Models) > len(ids) {
		rest = fmt.Sprintf(" и ещё %d", len(srv.Models)-len(ids))
	}
	return fmt.Sprintf("Найден локальный сервер %s — api_base взят из него. Модель не выбрана: выбери её в настройках (там %s%s) или впиши llm.model в .orchestra.yml",
		srv.APIBase, strings.Join(ids, ", "), rest)
}
