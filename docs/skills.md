# Skills

A *skill* is a reusable bundle of (system prompt + allowed tools + optional model/provider) stored as a single Markdown file with YAML frontmatter. Skills are the file-based, shareable form of the inline `agents:` block in `.orchestra.yml`.

## Location

`<project_root>/.orchestra/skills/<name>.md`

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
| `orchestra skills list` | List skills found under `.orchestra/skills/`. |
| `orchestra skills show <name>` | Print metadata + system prompt body. |
| `orchestra apply --skill <name> "<task>"` | Run apply with this skill's prompt + tool filter + model/provider overrides. |

## Rules

- `name` and `description` are required; `name` must not collide with a built-in mode (`build`, `plan`, `explore`, `general`, `compaction`, `title`, `summary`) or with any entry in `agents:` in `.orchestra.yml`.
- `tools` is optional; when omitted the skill inherits the full build toolset. When set, every name must be in the same allow-list as inline `agents:` (see `config.ValidAgentTool`).
- `model` is optional; overrides the model on the selected provider.
- `provider` is optional; must reference a key in the top-level `providers:` map in `.orchestra.yml`.
- `--skill` and `--mode` are mutually exclusive.
- Duplicate skill names across files cause `Discover` to error.
- Files with extensions other than `.md` and directories under `.orchestra/skills/` are ignored.

## Implementation notes

Internally a skill is materialised as a `config.AgentDefinition` and appended to `cfg.Agents` for the duration of the run, then resolved through the same code path that handles `--mode <name>` for inline custom agents. There is no separate "skill runtime" — same `agent.Options.SystemPromptOverride`, same `CustomTools` filter, same `--provider`/`Model` override semantics.
