package agent

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/internal/playbooks"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/llm"
)

// P1 in audit ledger (Sprint 6).
func (a *Agent) buildToolDefs() []llm.ToolDef {
	a.toolDefsOnce.Do(func() {
		a.toolDefsCache = a.computeToolDefs()
	})
	return a.toolDefsCache
}

func (a *Agent) computeToolDefs() []llm.ToolDef {
	var base []llm.ToolDef
	if len(a.opts.CustomTools) > 0 {
		// CustomTools replaces the mode-derived base; ExtraTools / skills
		// are still appended below.
		base = append(base, a.opts.CustomTools...)
	} else {
		caps := tools.Capabilities{
			Exec:    a.opts.AllowExec || len(a.opts.ExecAllow) > 0,
			Web:     a.opts.AllowWeb,
			Browser: a.opts.AllowBrowser,
		}
		hasSubtasks := a.opts.SubtaskRunner != nil
		hasQA := a.opts.QuestionAsker != nil
		switch {
		case a.opts.Mode != "":
			base = tools.ListToolsForMode(string(a.opts.Mode), caps, hasSubtasks, hasQA)
		case hasSubtasks:
			base = tools.ListToolsWithSubtasks(caps)
		default:
			base = tools.ListTools(caps)
		}
	}
	if len(a.opts.ExtraTools) > 0 {
		base = append(base, a.opts.ExtraTools...)
	}
	if a.opts.Mode != ModeOrchestra && a.opts.SkillRunner != nil && len(a.opts.Skills) > 0 {
		names := make([]string, len(a.opts.Skills))
		for i, s := range a.opts.Skills {
			names[i] = s.Name
		}
		base = append(base, tools.ToolSkillInvoke(names))
	}
	if a.opts.Mode == ModePlan || strings.TrimSpace(a.opts.PlanPath) != "" {
		for i := range base {
			base[i].Function.Description = a.substitutePlanPath(base[i].Function.Description)
		}
	}
	// Fast profile: drop LSP / browser tools unless CustomTools already fixed the set.
	if strings.EqualFold(a.opts.Profile, ProfileFast) && len(a.opts.CustomTools) == 0 {
		base = filterFastProfileTools(base)
	}
	// Mode lists and ExtraTools can overlap (e.g. orchestra mode ships repo_map
	// and core's ExtraTools appends it again). Strict providers (Anthropic)
	// reject requests with duplicate tool names, so keep the first occurrence.
	base = dedupToolDefs(base)
	if a.opts.Mode == ModeOrchestra {
		base = tools.FilterOrchestraLeadTools(base)
	}
	return base
}

// dedupToolDefs removes tools with duplicate names, keeping first occurrences.
func dedupToolDefs(in []llm.ToolDef) []llm.ToolDef {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, t := range in {
		if seen[t.Function.Name] {
			continue
		}
		seen[t.Function.Name] = true
		out = append(out, t)
	}
	return out
}

func filterFastProfileTools(in []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(in))
	for _, t := range in {
		name := t.Function.Name
		if strings.HasPrefix(name, "lsp.") || strings.HasPrefix(name, "browser.") {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (a *Agent) effectivePlanPath() string {
	if p := strings.TrimSpace(a.opts.PlanPath); p != "" {
		return plan.NormalizeRelPath(p)
	}
	return plan.NormalizeRelPath(".orchestra/plan.md")
}

func (a *Agent) substitutePlanPath(s string) string {
	return strings.ReplaceAll(s, "{{PLAN_PATH}}", a.effectivePlanPath())
}

// buildSystemPrompt assembles the system message handed to the LLM each
// step. The pipeline has five distinct stages  -  the first that fires
// becomes the *base*, the rest *append* on top:
//
//  1. BASE candidate: promptpkg.BuildSystemPromptForMode(Mode, PromptFamily)
//     -  the built-in prompt for the agent's mode.
//  2. BASE override: Options.SystemPromptOverride
//     -  when a custom agent declares a system_prompt in .orchestra.yml,
//     it REPLACES the mode default.
//  3. BASE override (highest precedence): .orchestra/system.txt in the
//     workspace root, loaded via promptpkg.LoadSystemOverride. If this
//     file exists it REPLACES whatever was selected above, including the
//     custom-agent prompt  -  file-system wins over config.
//  4. APPEND: project memory (ORCHESTRA.md + .orchestra/memory/*.md +
//     ~/.orchestra/memory.md + optional session layer) via internal/memory.
//  5. APPEND: <available_tools> catalog from live tool defs (names + short desc).
//  6. APPEND: the <available_skills> block (when a SkillRunner is wired).
//
// M10 in architecture audit: this used to live as five ad-hoc if-checks
// inline in nextStep. The replace-vs-append asymmetry (1/2/3 replace,
// 4/5/6 append) was implicit and easy to mis-order on edit. Moving it to
// a method documents the contract and centralises the order so a new
// prompt source can be added in one place.
// systemPromptParts holds the system prompt split by origin so the context
// popover can show a per-category token breakdown. Concatenation order in
// buildSystemPrompt: base + memory + catalog + skills.
type systemPromptParts struct {
	base    string // mode prompt / override
	memory  string // injected project memory (rules)
	catalog string // live tool catalog block
	skills  string // <available_skills> advertisement
}

func (a *Agent) buildSystemPromptParts() systemPromptParts {
	var p systemPromptParts
	// 1+2+3: base  -  first non-empty replacement wins (.orchestra/system.txt
	// > Options.SystemPromptOverride > mode default).
	p.base = promptpkg.BuildSystemPromptForMode(string(a.opts.Mode), a.opts.PromptFamily)
	if a.opts.SystemPromptOverride != "" {
		p.base = a.opts.SystemPromptOverride
	}
	// A per-mode file (.orchestra/system.<mode>.txt) overrides any mode.
	// The blanket .orchestra/system.txt does not reach child-only modes:
	// worker/verifier/product/documentation prompts carry the contract their
	// parent depends on (WorkOrder input, task_result output, scoped writes),
	// and an override written for build mode used to replace it silently.
	root := a.tools.WorkspaceRoot()
	if fs := promptpkg.LoadModeSystemOverride(root, string(a.opts.Mode)); fs != "" {
		p.base = fs
	} else if !IsChildOnlyMode(a.opts.Mode) {
		if fs := promptpkg.LoadSystemOverride(root); fs != "" {
			p.base = fs
		}
	}
	// 4: project memory (tiered, config-driven).
	// Workers/focused children skip this - they only need the WorkOrder.
	if !a.opts.SkipMemoryInject && a.opts.Mode != ModeWorker {
		memCfg := a.opts.Memory
		memCfg.Normalize()
		if a.opts.Mode == ModeOrchestra && memCfg.InjectKB > 2 {
			memCfg.InjectKB = 2 // Lead: ORCHESTRA.md header only; rest via memory_read
		}
		store := memory.NewStore(a.tools.WorkspaceRoot(), a.opts.SessionID, memCfg)
		var detail string
		p.memory, detail, _ = store.FormatInjectReport(memCfg.InjectBytes())
		a.opts.AgentLogger.LogMemoryInject(detail)
	}
	// 4a: top-level single-agent modes replay their own episodic lessons.
	// Without this the loop is half-built: recordTurnLesson writes what went
	// wrong and nobody ever reads it back, so build mode repeats the same
	// StaleContent or diagnostic every session. Workers and other child-only
	// modes are handed dept lessons by their spawner instead, and Orchestra
	// Lead gets the cross-dept catalog below.
	if !IsChildOnlyMode(a.opts.Mode) && a.opts.Mode != ModeOrchestra && !a.opts.SkipMemoryInject {
		dept := lessons.InferDeptFromFiles(a.working.ActiveFiles())
		if s := lessons.FormatInject(a.tools.WorkspaceRoot(), dept); s != "" {
			if p.memory != "" {
				p.memory += "\n\n" + s
			} else {
				p.memory = s
			}
		}
	}
	// 4b: Lead learning stack — capped so lessons+playbooks stay ≤ ~2000 tokens.
	// Dept Leads / workers get dept-scoped inject at spawn, not the full catalog.
	if a.opts.Mode == ModeOrchestra {
		root := a.tools.WorkspaceRoot()
		var extra strings.Builder
		if s := lessons.FormatLeadInject(root); s != "" {
			extra.WriteString(s)
		}
		if s := playbooks.FormatLeadPlaybooksInject(root, ""); s != "" {
			if extra.Len() > 0 {
				extra.WriteString("\n\n")
			}
			extra.WriteString(s)
		}
		if extra.Len() > 0 {
			clipped := clipLeadLearning(extra.String(), leadLearningMaxBytes)
			if p.memory != "" {
				p.memory += "\n\n" + clipped
			} else {
				p.memory = clipped
			}
		}
	}
	// 5: live tool catalog — a plain-text restatement of tools[] for models
	// that under-use the schema. It is not free: for build mode the block is
	// ~5 KB, 2.5× the base prompt itself, on top of ~32 KB of schemas already
	// on the wire. Families with reliable tool-calling (Anthropic, GPT, Gemini,
	// Kimi) get the schemas only; local/unknown models keep the catalog.
	// Orchestra Lead is excluded regardless — its 14-tool schema is small and
	// its prompt already enumerates the delegation surface.
	if a.opts.Mode != ModeOrchestra && needsToolCatalog(a.opts.PromptFamily) {
		p.catalog = formatToolsCatalog(a.buildToolDefs())
	}
	// 6: skills advertisement.
	if a.opts.Mode != ModeOrchestra {
		p.skills = a.skillsAdvertisement()
	}
	return p
}

func (a *Agent) buildSystemPrompt() string {
	p := a.buildSystemPromptParts()
	prompt := p.base
	if p.memory != "" {
		prompt += "\n\n" + p.memory
	}
	prompt += p.catalog
	prompt += p.skills
	// Always substitute: architecture.txt carries {{PLAN_PATH}} too, and mode
	// architecture with no explicit PlanPath used to ship the raw placeholder
	// to the model while its reminder showed the resolved path.
	// effectivePlanPath falls back to .orchestra/plan.md, so this is safe for
	// prompts that never mention the placeholder.
	prompt = a.substitutePlanPath(prompt)
	return prompt
}

// skillsAdvertisement returns a system-prompt block describing the
// skills available to the model. Empty when no skills are configured
// or no SkillRunner is wired.
func (a *Agent) skillsAdvertisement() string {
	if a.opts.SkillRunner == nil || len(a.opts.Skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<available_skills>\n")
	b.WriteString("You can invoke a skill via the skill_invoke tool when a subtask matches one. Pass {skill: <name>, task: <description>}; the result is returned synchronously.\n")
	for _, s := range a.opts.Skills {
		fmt.Fprintf(&b, "- %s  -  %s\n", s.Name, s.Description)
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// leadLearningMaxBytes caps <dept_lessons_all> + <dept_playbooks> (~2000 tokens at 4 B/tok).
const leadLearningMaxBytes = 8000

func clipLeadLearning(s string, maxBytes int) string {
	s = strings.TrimSpace(s)
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	if i := strings.LastIndex(cut, "\n"); i > maxBytes/2 {
		cut = cut[:i]
	}
	return cut + "\n...(truncated; use memory_read layer=lessons)"
}

// familiesWithReliableToolCalling do not need the <available_tools> restatement
// of the tool schemas: they call tools from tools[] dependably.
var familiesWithReliableToolCalling = map[string]bool{
	"anthropic": true,
	"gpt":       true,
	"gemini":    true,
	"kimi":      true,
}

// needsToolCatalog reports whether the model family needs the plain-text tool
// catalog in the system prompt. Unknown/empty family keeps it: an unrecognised
// model is more likely to be a local one that ignores tools[] than a frontier
// model, and a redundant catalog costs tokens while a missing one costs the run.
func needsToolCatalog(family string) bool {
	return !familiesWithReliableToolCalling[promptpkg.NormalizePromptFamily(family)]
}
