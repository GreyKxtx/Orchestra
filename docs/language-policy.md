# Language policy for LLM-facing strings

H5 in architecture audit asked: which language should the codebase speak?

## Decision

- **Code comments, commit messages, internal docs**: English (already the case).
- **LLM-facing agent prompts and error messages** (system prompts, denial hints, plan/build mode messages, dedup STOP messages, LSP error injections): **English**. Translated in commit `<this commit>`. Rationale: English is the lingua franca of code and chat-tuned LLMs are most reliable in English. Russian-only strings hurt non-Russian operators reading logs.
- **CLI user-facing strings** (cobra command Short/Long, --help, flag descriptions, "Loading models...", "Saved!"): **kept Russian for now**. These are USER UX, not LLM context. The audience is the operator at the terminal; if the operator base expands beyond Russian-speakers, switch then. Adding i18n machinery would over-engineer the current single-locale case.
- **Tool descriptions in `internal/tools/registry.go`**: **kept Russian for now**. These are read by the LLM, but the project default model (qwen3.6-27b) is multilingual; translation would touch 60+ description strings with uncertain quality gain. Revisit when default model changes.

## Going forward

When adding a new string that the LLM will see (system prompt, error hint, tool description, schema description that ends up in tool defs), write it in English. When adding a new CLI help text, follow the existing Russian convention until policy changes.

## Audit trail

Files touched in the H5 initial pass:
- `internal/agent/agent.go` — dup-call STOP, plan_exit refusal, plan-mode write denial, diagnostic-tracker escalation hint.
- `internal/agent/circuit_breaker.go` — RecordSuccessfulCall STOP hint.
- `internal/agent/format.go` — extractLSPErrors hint, formatValidatorErrorCompact, formatPolicyDeniedCompact.
- `internal/agent/error_format_test.go` — assertions updated to match new strings.

Deferred (separate work item):
- `internal/tools/registry.go` ~60 tool descriptions.
- `internal/ckg/provider.go` query/symbol/package response strings.
