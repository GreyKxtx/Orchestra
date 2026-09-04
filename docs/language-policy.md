# Language policy for LLM-facing strings

H5 in architecture audit asked: which language should the codebase speak?

## Decision

- **Code comments, commit messages, internal docs**: English (already the case).
- **LLM-facing agent prompts and error messages** (system prompts, denial hints, plan/build mode messages, dedup STOP messages, LSP error injections): **English**. Translated in commit `<this commit>`. Rationale: English is the lingua franca of code and chat-tuned LLMs are most reliable in English. Russian-only strings hurt non-Russian operators reading logs.
- **CLI user-facing strings** (cobra command Short/Long, --help, flag descriptions, "Loading models...", "Saved!"): **kept Russian for now**. These are USER UX, not LLM context. The audience is the operator at the terminal; if the operator base expands beyond Russian-speakers, switch then. Adding i18n machinery would over-engineer the current single-locale case.
- **Tool descriptions and tool output**: **English** (revised — see below).

## Going forward

When adding a new string that the LLM will see (system prompt, error hint, tool description, schema description that ends up in tool defs), write it in English. When adding a new CLI help text, follow the existing Russian convention until policy changes.

## Audit trail

Files touched in the H5 initial pass:
- `internal/agent/agent.go` — dup-call STOP, plan_exit refusal, plan-mode write denial, diagnostic-tracker escalation hint.
- `internal/agent/circuit_breaker.go` — RecordSuccessfulCall STOP hint.
- `internal/agent/format.go` — extractLSPErrors hint, formatValidatorErrorCompact, formatPolicyDeniedCompact.
- `internal/agent/error_format_test.go` — assertions updated to match new strings.

Deferred (separate work item):
- `internal/ckg/provider.go` query/symbol/package response strings.

## Revision: tool descriptions are English after all

The original decision deferred ~60 tool descriptions on the grounds that the
default model is multilingual and the gain was uncertain. Two things changed:

1. Every prompt file was translated to English, with a test that fails on any
   Cyrillic. That left a single turn changing language between the system
   prompt and the tool schema sitting next to it.
2. The schemas turned out to be the larger half of the wire — roughly 32 KB of
   tool definitions against a ~2 KB system prompt for a cloud family — so the
   tokenisation penalty on Cyrillic lands on every request of every run, not
   just on the prompt.

All 58 tool definitions and the tool output the model reads back (grep results,
the .go read redirect) are now English, pinned by
`TestToolDefinitions_AreEnglish` in `internal/tools`.

Still Russian by policy, unchanged: CLI help, and the interactive `question`
prompts in `internal/tools/session/question.go` — those are read by the
operator at the terminal, not by the model.
