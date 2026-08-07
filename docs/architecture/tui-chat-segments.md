# TUI chat segments (chronological parts)

Assistant turns store **chronological segments** (Claude Code / OpenCode parts model), not a fixed stack of Reasoning → all tools → Text → notices-at-bottom.

## Model

```go
type Segment struct {
  Kind  // reasoning | text | tools | notice
  Text  string
  Tools []ToolBlock // contiguous tool run
  NoticeKind SystemKind // only for notice
}
```

One user turn = one `Message` (footer / Ctrl+T / Ctrl+R unchanged). Inside: ordered `Segments`.

## Event mapping

| Event | Effect |
|-------|--------|
| `reasoning_delta` | append / open `SegmentReasoning` |
| `message_delta` | append / open `SegmentText` |
| `tool_call_start` | append / open `SegmentTools` |
| `recoverable_error` / compact | append `SegmentNotice` **in place** (not footer dump) |
| `step_done` ≠ final | **no truncate** — mid-step text stays |
| `AgentRunCompleted` | `FinishAssistant` |

## Persist

Session schema **v3**: `ui_messages[].segments`. Flat `text` / `reasoning` / `tool_blocks` / `notices` kept as projections for older readers. Load synthesizes segments from flat fields when `segments` is empty (old order). Flat `notices` without `kind=notice` segments are migrated onto the end of the segment list.

## Code

- `ui/tui/state/segment.go`, `session.go`
- `ui/tui/view/message_assistant.go`, `notice.go`
- `internal/sessionstore/convert.go`, `internal/sessionfile`
