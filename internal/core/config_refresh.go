package core

import (
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// noteConfigMTime records the on-disk mtime of .orchestra.yml after this
// process loaded or saved it, so RefreshConfigIfChanged can tell our own
// writes apart from external ones (TUI in a terminal, CLI, manual edits).
func (c *Core) noteConfigMTime() {
	if c == nil {
		return
	}
	path := c.configFilePath()
	if strings.TrimSpace(path) == "" {
		return
	}
	if st, err := os.Stat(path); err == nil {
		c.cfgMu.Lock()
		c.cfgMTime = st.ModTime()
		c.cfgMu.Unlock()
	}
}

func (c *Core) knownConfigMTime() time.Time {
	c.cfgMu.RLock()
	defer c.cfgMu.RUnlock()
	return c.cfgMTime
}

// RefreshConfigIfChanged reloads .orchestra.yml when another client modified
// it while this core process is running. Every frontend of the same project
// (VS Code extension, TUI, CLI) then works against one shared config instead
// of clobbering each other's changes with stale in-memory copies.
//
// Called from the RPC handler before dispatch. Skipped while an agent turn
// holds runMu: hot-swapping config mid-turn would race the running agent.
func (c *Core) RefreshConfigIfChanged() {
	if c == nil || c.cfg == nil {
		return
	}
	path := c.configFilePath()
	if strings.TrimSpace(path) == "" {
		return
	}
	st, err := os.Stat(path)
	if err != nil {
		return
	}
	if st.ModTime().Equal(c.knownConfigMTime()) {
		return
	}
	if !c.runMu.TryLock() {
		return // agent turn in flight; retried on the next RPC
	}
	defer c.runMu.Unlock()

	fresh, err := config.Load(path)
	if err != nil {
		// Half-written file (concurrent save) — keep the current config;
		// the next RPC retries because mtime still differs.
		return
	}
	oldLLM := c.cfg.LLM
	c.cfgMu.Lock()
	c.cfg = fresh
	c.cfgMTime = st.ModTime()
	c.cfgMu.Unlock()

	// Rebuild the LLM client when the active model/provider changed on disk.
	if !c.llmClientInjected && !reflect.DeepEqual(oldLLM, fresh.LLM) {
		client := llm.NewClient(fresh.LLM)
		if oc, ok := llm.AsOpenAIClient(client); ok {
			oc.SetLogger(llm.NewLogger(c.workspaceRoot))
		}
		c.llmClient = client
	}
	// Push refreshed exclude dirs + embed credentials into the tools runner.
	c.applyEmbedRuntime()
}
