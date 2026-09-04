# Prompt cache

An agent step re-sends the entire conversation. Providers bill that at full
price unless the request shares a byte-identical **prefix** with a recent one,
in which case the shared part is served from a prompt cache (Anthropic: ~10% of
the input price on a read, 125% on the write; OpenAI/DeepSeek: automatic, same
prefix rule). On a 30-step run over a 100k-token history the difference is
roughly an order of magnitude in cost, plus a large cut in time-to-first-token.

## The prefix must not move

`Agent.nextStep` (`internal/agent/agent_step.go`) builds every request as:

```
system            ← buildSystemPrompt: base + project memory + tool catalog
user  (stable)    ← BuildUserPrompt(query, snapshot) [+ CKG context on step 1]
…history…         ← append-only: assistant tool_calls + tool results
user  (volatile)  ← todos, <working_state>, turn digests, mode reminder
```

The volatile block changes on nearly every step — `working.ObserveTool` updates
it after each tool call. It therefore goes **last**, after the history. Placed
in the leading user message (where it used to live) it broke the common prefix
at message #2, so nothing past the system block could ever be cached and each
step re-paid for the whole transcript.

Two rules follow, and both are covered by
`internal/agent/prompt_cache_prefix_test.go`:

- anything that varies step to step is appended after the history;
- the system prompt and the first user message stay byte-identical for the
  whole run (step 1 differs by design: it carries the one-shot CKG context).

Compaction necessarily invalidates the cache — it rewrites the prefix. That is
another reason to compact late rather than at a fixed 60% (see
`config.AutoCompactThresholdPct`).

## Cache breakpoints (Anthropic)

The Anthropic API caches only up to explicitly marked blocks. `llm/anthropic.go`
sets three breakpoints, well inside the limit of four:

| Where | Function | Why |
|-------|----------|-----|
| System block | `CompleteStream` | Base prompt + memory + tool catalog, stable per run |
| Last tool schema | `markToolsCacheBreakpoint` | Tool defs are identical every step and are several KB |
| Last message before the volatile tail | `markPrefixCacheBreakpoint` | Rolling breakpoint: each step reads the previous prefix and writes only what was appended |

Anthropic requires alternating roles, so `convertToAnthropic` folds the trailing
volatile user message into the preceding user message as an extra text block.

Most OpenAI-compatible providers need no markers — they match the prefix
automatically, so the ordering rule above is what matters there.

## Cache breakpoints through a gateway (`llm/prompt_cache_gateway.go`)

Anthropic models reached through OpenRouter do **not** take the native path:
they go out as OpenAI-compatible requests, where the only way to ask for
caching is an Anthropic-shaped `cache_control` block inside array-form content.
OpenRouter forwards it to the underlying API verbatim.

Two breakpoints are set, mirroring the native path: the system block, and the
last message before the volatile tail. Messages carrying only `tool_calls` are
left alone — rewriting their content into an array would drop the calls.

The marker is only sent for models named `anthropic/…`. Other providers cache
by prefix and gain nothing, and a self-hosted OpenAI-compatible server may
reject an unknown field. If an endpoint rejects it anyway, the client retries
once without markers and latches them off for the rest of its life
(`promptCacheDisabled`), so a gateway that does not understand the field costs
one retry rather than failing every remaining step.

## Checking that it works

Each step emits `StreamEventStepUsage` with `cached_prompt_tokens` and
`cache_write_tokens` (`Agent.emitStepUsage`), parsed from
`cache_read_input_tokens` / `cache_creation_input_tokens` (Anthropic) and
`prompt_tokens_details.cached_tokens` (OpenAI-compatible).

On a healthy long run `cached_prompt_tokens` grows with the history and
approaches `prompt_tokens`. If it stays at 0 past step 2, something is mutating
the prefix — check what was added to the system prompt or the first user message.
