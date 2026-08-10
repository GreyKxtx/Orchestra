package agent

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/plan"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/internal/memory"
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
	if a.opts.SkillRunner != nil && len(a.opts.Skills) > 0 {
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
	return base
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
//      -  the built-in prompt for the agent's mode.
//  2. BASE override: Options.SystemPromptOverride
//      -  when a custom agent declares a system_prompt in .orchestra.yml,
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
func (a *Agent) buildSystemPrompt() string {
	// 1+2+3: base  -  first non-empty replacement wins (.orchestra/system.txt
	// > Options.SystemPromptOverride > mode default).
	prompt := promptpkg.BuildSystemPromptForMode(string(a.opts.Mode), a.opts.PromptFamily)
	if a.opts.SystemPromptOverride != "" {
		prompt = a.opts.SystemPromptOverride
	}
	if fs := promptpkg.LoadSystemOverride(a.tools.WorkspaceRoot()); fs != "" {
		prompt = fs
	}
	// 4: append project memory (tiered, config-driven).
	// Workers/focused children skip this - they only need the WorkOrder.
	if !a.opts.SkipMemoryInject && a.opts.Mode != ModeWorker {
		memCfg := a.opts.Memory
		memCfg.Normalize()
		store := memory.NewStore(a.tools.WorkspaceRoot(), a.opts.SessionID, memCfg)
		if block := store.FormatInject(memCfg.InjectBytes()); block != "" {
			prompt += "\n\n" + block
		}
	}
	// 5: live tool catalog (mode/caps accurate  -  better than hardcoded lists in *.txt).
	prompt += formatToolsCatalog(a.buildToolDefs())
	// 6: append skills advertisement.
	prompt += a.skillsAdvertisement()
	if a.opts.Mode == ModePlan || strings.TrimSpace(a.opts.PlanPath) != "" {
		prompt = a.substitutePlanPath(prompt)
	}
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