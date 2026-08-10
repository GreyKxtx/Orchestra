# Streaming architecture

Orchestra streams LLM output and tool progress from the agent loop to CLI, TUI, and JSON-RPC clients.

## Data flow

```
LLM provider (SSE)
    → llm.CompleteStream / ParseSSEStream
    → agent.streamStep (always when client implements Streamer)
    → Options.OnEvent (optional — UI only)
    → core.buildAgentOnEvent
    → JSON-RPC notification agent/event
    → TUI / VS Code / CLI renderer
```

## LLM layer (`llm/`)

| Component | Role |
|-----------|------|
| `Streamer` interface | `CompleteStream(ctx, req) (<-chan StreamEvent, error)` |
| `OpenAIClient` | OpenAI-compatible SSE (`stream:true`); `Complete()` drains `CompleteStream` |
| `AnthropicClient` | Anthropic Messages API SSE (`stream:true`); same drain pattern |
| `ParseSSEStream` | OpenAI/vLLM/LM Studio chunk assembly + `tool_calls` by index |
| `ParseAnthropicSSEStream` | Anthropic `event:`/`data:` pairs; text + tool_use JSON deltas |
| `DrainStreamEvents` | Collect `Done` response from a stream channel |

**Important:** The agent always prefers `CompleteStream` when the client implements `Streamer`. The legacy non-streaming OpenAI POST is no longer used for `Complete()` — this fixes LM Studio/vLLM tool-call assembly bugs on the blocking path.

## Agent layer (`internal/agent/`)

- `nextStep` calls `streamStep` whenever `a.llm.(llm.Streamer)` succeeds.
- `OnEvent == nil` disables **UI forwarding only**; LLM streaming still runs.
- Tool results emit `tool_call_completed`; `bash`/`exec.run` emits `exec_output` chunks when `OnEvent` is set.

## JSON-RPC (`internal/core/agent_events.go`)

Notifications on method `agent/event` (and `exec/output_chunk` for bash):

| `type` | Meaning |
|--------|---------|
| `message_delta` | Assistant text token |
| `reasoning_delta` | Chain-of-thought token (Qwen/DeepSeek field) |
| `tool_call_start` / `tool_call_delta` | Tool invocation |
| `tool_call_completed` | Tool finished (preview in `content`) |
| `step_done` | Agent loop iteration finished |
| `recoverable_error` | Retry hint (stale content, validation, …) |
| `pending_ops` | Dry-run patch preview |

Spec: `docs/PROTOCOL.md` § Streaming events.

## CLI (`orchestra apply`)

- **TTY** (stdout or stderr): tokens on stderr, tools as `→ name` / `← preview`.
- **`--stream`**: tokens on stdout (pipe-friendly); forces display even without TTY.
- **`NO_COLOR`**: suppresses CLI renderer (streaming still runs internally).

## Debugging

- `ORCH_STREAM_DEBUG=/path/to/log` — append raw SSE `data:` lines (`llm/stream.go`).

## Provider notes

| Provider | Streaming | Notes |
|----------|-----------|-------|
| OpenAI-compatible (LM Studio, vLLM, OpenRouter) | ✅ | `tool_calls` may arrive in one chunk or split by index — parser handles both |
| Anthropic | ✅ | `input_json_delta` for tool args |
| Mock LLM (tests) | `Complete` only | Integration tests inject `Complete`; E2E uses real subprocess core |

---

## Operational notes (backpressure, cancel, partial streams)

Production streaming has four classic edge cases. Below: **what Orchestra does today** vs **optional follow-ups**.

### 1. Backpressure (slow consumer)

**Chain:** `ParseSSEStream` → buffered `chan StreamEvent` (cap 16) → `agent.streamStepOnce` → synchronous `OnEvent` → `core` `Notify` → stdio write → client read loop.

| Layer | Behavior |
|-------|----------|
| `llm` | SSE parser goroutine blocks on full channel (16 slots). HTTP body read stalls → natural backpressure to provider. |
| `agent` | `OnEvent` is **synchronous** — a slow callback blocks the stream read loop. |
| `core` | `buildAgentOnEvent` → `notifier.Notify` writes one JSON-RPC frame per event (mutex on Writer). |
| **TUI** (`ui/tui/rpcclient`) | **Coalesces** `message_delta` and `tool_call_delta` when the events channel is saturated; drops non-mergeable events only after one non-blocking try. |
| **VS Code** | Handles each notification in the extension host; no core-level coalesce yet. |

**Verdict:** Safe for correctness (LLM won't run unbounded ahead of a stuck client), but a very slow UI can slow token delivery. TUI mitigates locally. Optional improvement: async notify queue in `core` or time-based debounce in `buildAgentOnEvent` (~20–50 ms for `message_delta` only).

### 2. Partial tool call / mid-stream failure

If the SSE stream dies before `StreamEventDone`:

- `streamStepOnce` returns **error**, no `CompleteResponse` → **nothing is appended to agent history** for that step.
- `contentStarted` flag: if any text/tool delta was already forwarded, **transient retries are skipped** (avoids duplicate UI tokens); user sees `StreamEventError` or `recoverable_error` instead.
- If the model completed but JSON args are invalid → `NormalizeLLM` fails → validation error injected as user message, **retry inside the same step** (orphan tool_call never committed).
- History repair: `sanitizeOrphanedToolCalls` strips assistant `tool_calls` without matching tool replies after truncate/compaction.
- **TUI:** on `step_done` with `reason != "final"`, assistant scratch text is truncated back to `stepTextLen` so half-finished pre-tool chatter doesn't stick in the viewport.

**Verdict:** Broken streams do not poison LLM context. UI may briefly show partial deltas until error/step_done cleanup.

### 3. Graceful cancellation

| Trigger | Path |
|---------|------|
| `session.cancel` | Cancels per-session turn context → propagates to `Agent.Run` → `streamStep` → `CompleteStream(ctx, …)`. |
| TUI Esc | RPC cancel + local turn FSM reset. |
| LLM step timeout | `context.WithTimeout` on each step; error surfaced as `LLM step timed out…`. |

**HTTP:** `OpenAIClient.streamOnce` uses `http.NewRequestWithContext(streamCtx, …)` where `streamCtx` is derived from the caller ctx. Cancel closes the transport and unblocks `ParseSSEStream`. Stall watchdog also calls `cancelStream()` on idle timeout.

**Caveat:** `ParseSSEStream` checks `ctx` between SSE lines; a blocked `Scan()` on one line unblocks when the body closes on cancel (documented in `llm/stream.go`).

### 4. Event granularity (debouncing)

- **Core** emits **one RPC notification per stream event** (no debounce).
- **TUI client** merges consecutive `message_delta` / same-id `tool_call_delta` under backpressure.
- **CLI apply** writes tokens directly to stderr/stdout (no RPC).

Optional: core-side debounce for `message_delta`/`reasoning_delta` only, preserving immediate delivery for tool boundaries and `step_done`.

---

## Related docs

- Multi-turn clients (TUI, VS Code): `docs/architecture/tui-pipeline.md` — **not** a separate `orchestra chat` CLI.
- Session RPC: `docs/PROTOCOL.md`.
