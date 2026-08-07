# Skills

A *skill* is a reusable bundle of (system prompt + allowed tools + optional model/provider) stored as a single Markdown file with YAML frontmatter. Skills are the file-based, shareable form of the inline `agents:` block in `.orchestra.yml`.

## Location

Skills are discovered from three sources (all optional, override precedence high → low):

1. `<project_root>/.orchestra/skills/<name>.md` — project-local.
2. `<user_home>/.orchestra/skills/<name>.md` — shared across all your projects.
3. `<user_home>/.orchestra/packs/<pack-id>/**/<name>.md` — installed third-party packs.

Use the project dir for repo-specific guidance, the user dir for skills you want everywhere, packs for skills shared by a team or community.

A skill file:

```markdown
---
name: refactor-go
description: Refactor Go code with conservative edits.
tools: [read, edit, write, grep, symbols]
model: qwen3.6-27b
provider: lmstudio   # optional; references providers: in .orchestra.yml
---
You are a careful Go refactoring assistant.
Make small, focused edits. Run tests after each change.
```

## Commands

| Command | Effect |
|---|---|
| `orchestra skills list` | List skills with `NAME / ORIGIN / DESCRIPTION` columns. |
| `orchestra skills show <name>` | Print metadata + system prompt body. |
| `orchestra apply --skill <name> "<task>"` | Run apply with this skill's prompt + tool filter + model/provider overrides. |
| `orchestra skills install <git\|archive\|path> [--yes]` | Fetch a third-party pack to `~/.orchestra/packs/`, review each skill interactively (`--yes` to skip prompts; dangerous). |
| `orchestra skills uninstall <pack-id>` | Remove an installed pack. |

## Security — packs

A skill body becomes the system prompt of a child agent with full tool access. **Installing a pack is equivalent to letting its author write your agent's instructions**, similar in trust level to running an `npm install` postinstall script (and arguably worse because the LLM can do more with tool budget than a sandboxed script).

`install` therefore prompts you per skill, showing the full body before it goes live. Vet by hand on first install of any pack you don't already trust. `--yes` exists for automation but emits a `WARNING:` line.

Packs live under their own pack id (`pack:<id>` in `skills list`), separate from the user/project dirs — so audit trails stay clean.

## `$ARGUMENTS` substitution

If the body contains the literal token `$ARGUMENTS`, it is replaced with the user query before the body becomes the system prompt. Useful for templated skills:

```markdown
---
name: refactor
description: Refactor a single file.
tools: [read, edit, write]
---
Refactor the following file with small, focused edits, then run tests:
$ARGUMENTS
```

`orchestra apply --skill refactor "internal/foo.go"` → the system prompt becomes `Refactor the following file ... internal/foo.go`. Skills without the marker pass the query through unchanged as the regular user message.

## LLM-invokable skills (`skill_invoke`)

When `.orchestra/skills/` is non-empty, every `orchestra apply` run automatically exposes a `skill_invoke` tool to the model and lists available skills in the system prompt:

```
<available_skills>
- refactor — Refactor a single file.
- review — Review changes for risks.
</available_skills>
```

The model invokes a skill mid-run with `skill_invoke{skill: "<name>", task: "<text>"}`. A fresh child agent is spawned synchronously with that skill's body as the system prompt (with `$ARGUMENTS` substituted from `task`), its tools as the tool filter, and its `model`/`provider` overrides applied. The child cannot recursively spawn skills or subtasks. The tool returns the child's final result text.

Use `--skill <name>` (CLI-driven) when you want to commit a whole run to one skill from the start; use the in-process `skill_invoke` tool when the *parent* agent should orchestrate and only delegate certain subtasks.

## Rules

- `name` and `description` are required; `name` must not collide with a built-in mode (`build`, `plan`, `explore`, `ask`, `debug`, `architecture`, `general`, `agent`, `orchestra`, `worker`, `compaction`, `title`, `summary`) or with any entry in `agents:` in `.orchestra.yml`.
- `tools` is optional; when omitted the skill inherits the full build toolset. When set, every name must be in the same allow-list as inline `agents:` (see `config.ValidAgentTool`).
- `model` is optional; overrides the model on the selected provider.
- `provider` is optional; must reference a key in the top-level `providers:` map in `.orchestra.yml`.
- `--skill` and `--mode` are mutually exclusive.
- Duplicate skill names across files cause `Discover` to error.
- Files with extensions other than `.md` and directories under `.orchestra/skills/` are ignored.

## Implementation notes

- CLI path (`--skill`) materialises the skill as a `config.AgentDefinition`, appends it to `cfg.Agents` for the run, and resolves it through the same code as `--mode <name>` for inline custom agents.
- In-process path (`skill_invoke`) uses `agent.SkillRunner` (impl in `internal/cli/skill_runner.go`) to spawn a child agent with the skill's prompt/tools/model/provider, returning the result synchronously. The child runs with `SubtaskRunner=nil` and `SkillRunner=nil` to prevent recursive spawning.
- Tool-name validation is shared via `config.ValidAgentTool`.
