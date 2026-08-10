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
