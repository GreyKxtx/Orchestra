# TUI Phase 0 — Streaming Events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Расширить существующий streaming-протокол `agent/event` четырьмя недостающими типами событий (`tool_call_completed`, `step_done`, `pending_ops`, `recoverable_error`) и добавить bidirectional метод `permission/request` ↔ `permission/response`. Подготовить ядро к подключению будущего TUI без касания UI-слоя.

**Architecture:** Большая часть инфраструктуры уже есть: `jsonrpc.Server.Notify`, `Client.SetNotificationHandler`, `Agent.Options.OnEvent`, `Core.AgentRunParams.OnEvent`. Текущий `OnEvent` эмитит LLM-уровневые события (`message_delta`, `tool_call_start/delta`, `done`, `error`) и `exec/output_chunk`. Добавляем агент-уровневые события: после успешного `tools.Call`, после `final` с патчами, после каждого шага, при recoverable resolve-ошибках. `permission/request` — server-initiated request: использует расширенный `jsonrpc.Server` с методом `Request` и обработчиком ответов клиента.

**Tech Stack:** Go (existing). Никаких новых зависимостей.

**Reference:** [`docs/superpowers/specs/2026-05-07-tui-design.md`](../specs/2026-05-07-tui-design.md), секции «Streaming-события в JSON-RPC».

## Workflow Note (важно для исполнителей)

User preference (`memory/user_prefs.md`): **сначала вся реализация, потом тестовая фаза**. Don't stop to fix failing tests mid-implementation. Поэтому:

- **Tasks 1-7:** только реализация + smoke-build (`go build ./...`). Без написания новых тестов. Если существующие тесты падают из-за рефакторинга — отметить и продолжать.
- **Task 8:** единый test audit pass — написать все недостающие тесты, починить существующие сломавшиеся.
- **Task 9:** documentation + version bump.

Subagent должен следовать этому правилу: не писать тесты в задачах 1-7, даже если кажется естественным. Все тесты собираются в Task 8.

---

## File Structure

| Файл | Создаётся / Изменяется | Ответственность |
|---|---|---|
| `internal/llm/stream.go` | Modify | Добавить `StreamEventKind` константы для агент-уровневых событий |
| `internal/agent/agent.go` | Modify | Эмиссия `tool_call_completed`, `step_done`, `pending_ops`, `recoverable_error` через `Options.OnEvent`; интеграция `PermissionRequester` |
| `internal/agent/types.go` | Modify (опционально) | Если потребуется расширить `AgentEvent` |
| `internal/core/core.go` | Modify | Маппинг новых `StreamEventKind` → JSON payload в `AgentRun`/`SessionMessage` (lines ~348-369 и ~674-690); `PermissionRequester` в `AgentRunParams` |
| `internal/core/permissions.go` | Create | `PermissionRequester` interface + `rpcPermissionRequester` impl |
| `internal/core/rpc_handler.go` | Modify | `SetRequester` для проброса в `PermissionRequester` |
| `internal/jsonrpc/server.go` | Modify | `Server.Request(method, params, result)` — server-initiated request с pending map + ответами клиента |
| `internal/jsonrpc/client.go` | Modify | `SetRequestHandler` — клиент отвечает на server-initiated requests |
| `internal/protocol/version.go` | Modify | Бамп `ProtocolVersion` 2 → 3 |
| `docs/PROTOCOL.md` | Modify | Документация всех новых типов событий + `permission/request` |
| `internal/agent/agent_test.go` | Modify | Новые тесты на агент-уровневые события и permission flow (Task 8) |
| `internal/jsonrpc/server_test.go` | Modify | Тест `Server.Request` round-trip (Task 8) |
| `internal/jsonrpc/client_test.go` | Modify | Тест `Client.SetRequestHandler` (Task 8) |
| `internal/core/rpc_handler_test.go` | Modify | Integration test событий через rpc handler (Task 8) |

---

## Task 1: Add agent-level StreamEventKind constants

**Files:**
- Modify: `internal/llm/stream.go`

- [ ] **Step 1: Add constants**

В `internal/llm/stream.go` после `StreamEventExecOutput` (line 34) добавить:

```go
const (
	// ... existing ...

	// StreamEventToolCallCompleted is emitted by the agent loop after a tools.Call returns.
	// Content holds a short result preview (truncated to 256 bytes).
	// ToolCallID/ToolCallName identify the call.
	StreamEventToolCallCompleted StreamEventKind = "tool_call_completed"

	// StreamEventStepDone is emitted at the end of each agent loop iteration.
	// Content holds the reason: "tool_call", "final", "invalid", "compaction".
	StreamEventStepDone StreamEventKind = "step_done"

	// StreamEventPendingOps is emitted when the agent produces final patches (dry-run or pre-apply).
	// Content holds a JSON-encoded {ops: [...], diff: "...", applied: bool} payload.
	StreamEventPendingOps StreamEventKind = "pending_ops"

	// StreamEventRecoverableError is emitted when a non-fatal error (StaleContent, AmbiguousMatch,
	// schema validation failure) occurs and the loop will retry. Content holds a short message.
	StreamEventRecoverableError StreamEventKind = "recoverable_error"
)
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean build, no warnings.

- [ ] **Step 3: Commit**

```bash
git add internal/llm/stream.go
git commit -m "feat(llm): add agent-level StreamEventKind constants for TUI streaming

Adds tool_call_completed, step_done, pending_ops, recoverable_error.
These will be emitted by the agent loop (not by llm.CompleteStream)
to surface agent-level state changes for streaming clients (TUI).

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Emit `tool_call_completed` after Runner.Call in agent loop

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Locate the call site**

В `internal/agent/agent.go` найти строку вида `out, err := a.tools.Call(callCtx, name, step.Tool.Input)` (около line 586). Это единственное место где агент исполняет tool call.

- [ ] **Step 2: Add emission after the call**

Сразу после получения `out, err` (но до их обработки/возврата) вставить:

```go
// Emit tool_call_completed event for streaming clients.
if a.opts.OnEvent != nil {
	preview := ""
	if len(out) > 0 {
		const maxPreview = 256
		if len(out) > maxPreview {
			preview = string(out[:maxPreview]) + "...(truncated)"
		} else {
			preview = string(out)
		}
	}
	if err != nil {
		preview = "error: " + err.Error()
		if len(preview) > 256 {
			preview = preview[:256] + "...(truncated)"
		}
	}
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:         llm.StreamEventToolCallCompleted,
		ToolCallName: name,
		ToolCallID:   step.Tool.CallID,
		Content:      preview,
	}})
}
```

**Подгонка под код:**
- `stepIdx` — реальное имя переменной шага в этой функции (посмотрите контекст — может быть `step`, `stepNum`, `i` и т.п.)
- `step.Tool.CallID` — поле id вызова. Если в структуре `step.Tool` нет `CallID` — посмотрите какое поле служит идентификатором (`ID`, `ToolUseID` и т.п.)
- Если в `llm.StreamEvent` нет полей `ToolCallID`/`ToolCallName` (проверьте `internal/llm/stream.go`) — используйте только `Content` со склейкой, или добавьте поля в struct (тогда это отдельный коммит до этой задачи)

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): emit tool_call_completed event after Runner.Call

Streaming clients (TUI) need to know when a tool call finished, not just
when the LLM declared its intent (existing tool_call_start). Emits via
Options.OnEvent with truncated result preview (256 bytes).

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Emit `step_done` at end of each loop iteration

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Identify loop iteration boundaries**

В `internal/agent/agent.go::Run`, найти главный цикл агента (`for stepIdx := 0; stepIdx < a.opts.MaxSteps; stepIdx++` или эквивалент). Каждая итерация цикла завершается одним из:
- успешный tool call → `continue`
- `final` патчи обработаны (success или final-failure) → `continue` или `return`
- schema invalid → `continue` (с retry counter)
- compaction triggered → `continue`

Цель: эмитить `step_done` ровно один раз перед каждым `continue`/`return` в этом цикле.

- [ ] **Step 2: Introduce stepReason variable**

В начале каждой итерации цикла (сразу после `for stepIdx := ...`) объявить:

```go
stepReason := ""
```

В каждой ветке завершения итерации присвоить:
- после успешного tool dispatch: `stepReason = "tool_call"`
- после успешного final: `stepReason = "final"`
- после schema invalid retry: `stepReason = "invalid"`
- после compaction: `stepReason = "compaction"`
- после recoverable resolve error: `stepReason = "final_retry"`

- [ ] **Step 3: Emit at iteration boundary**

Самый чистый способ — использовать `defer` в начале функции `Run`:

```go
emitStepDone := func(stepIdx int, reason string) {
	if a.opts.OnEvent != nil && reason != "" {
		a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
			Kind:    llm.StreamEventStepDone,
			Content: reason,
		}})
	}
}
```

И вызывать `emitStepDone(stepIdx, stepReason)` непосредственно перед каждым `continue` / `return` внутри основного цикла (где это применимо — для нормального завершения шага).

**Не эмитить** для:
- ошибок которые приводят к падению `Run` с err (например MaxFinalFailures exceeded — это не нормальное завершение шага)

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): emit step_done event at end of each loop iteration

Reason field captures why the step ended: tool_call | final | invalid |
compaction | final_retry. Streaming clients use this for progress
indication and to know when the agent is between LLM calls.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Emit `recoverable_error` for StaleContent/AmbiguousMatch/schema-invalid

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Locate StaleContent/AmbiguousMatch branch**

Около line 668-669 в `internal/agent/agent.go`:

```go
// StaleContent/AmbiguousMatch are recoverable: keep looping.
if pe, ok := protocol.AsError(err); ok && (pe.Code == protocol.StaleContent || pe.Code == protocol.AmbiguousMatch) {
    // ...
}
```

- [ ] **Step 2: Add emission inside the recoverable branch**

Внутри этой ветки (до `continue`) добавить:

```go
if a.opts.OnEvent != nil {
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventRecoverableError,
		Content: fmt.Sprintf("%s: %s", pe.Code, pe.Message),
	}})
}
```

- [ ] **Step 3: Locate schema validation invalid branch**

Найти где увеличивается счётчик `MaxInvalidRetries` (поиск по `MaxInvalidRetries` в agent.go). Это ветка обработки невалидного JSON/schema от LLM.

- [ ] **Step 4: Add emission for invalid retry**

Перед `continue` в этой ветке:

```go
if a.opts.OnEvent != nil {
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventRecoverableError,
		Content: "schema invalid: " + truncate(invalidErrMsg, 200),
	}})
}
```

`truncate` — простой helper или inline-truncate если в файле такой helper уже есть.

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): emit recoverable_error events before retry

StaleContent/AmbiguousMatch from resolver and schema-invalid responses
from LLM are non-fatal — the loop retries. Streaming clients get visibility
into what the agent is recovering from instead of seeing only the next
LLM call.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Emit `pending_ops` event when agent finalizes patches

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Locate FSApplyOps call**

Около line 658-665:

```go
applyReq := tools.FSApplyOpsRequest{...}
resp, err := a.tools.FSApplyOps(ctx, applyReq)
```

- [ ] **Step 2: Determine resolved ops variable**

Перед `applyReq` ops уже резолвнуты из external patches. Имя переменной — обычно `ops` или `resolvedOps`. Проверить в контексте: `resolver.ResolveExternalPatches(...)` возвращает `[]ops.AnyOp`.

- [ ] **Step 3: Add emission after successful FSApplyOps**

Сразу после успешной проверки `if err != nil` (т.е. в success path), но до `return`:

```go
if a.opts.OnEvent != nil && resp != nil {
	payload := map[string]any{
		"ops":     resolvedOps,
		"diff":    resp.Diff,         // имя поля проверить в tools.FSApplyOpsResponse
		"applied": a.opts.Apply,
	}
	payloadJSON, _ := json.Marshal(payload)
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventPendingOps,
		Content: string(payloadJSON),
	}})
}
```

**Подгонка:**
- `resolvedOps` — реальное имя переменной с `[]ops.AnyOp` после resolve
- Поле diff в `tools.FSApplyOpsResponse` может называться `Diff`, `UnifiedDiff` или быть собранным из `Files[].Diff` — посмотреть в `internal/tools/`

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): emit pending_ops event with ops + diff payload

After agent emits 'final' with patches and they resolve+apply (dry-run
or write), emit a pending_ops streaming event carrying the resolved ops
and unified diff. TUI uses this to render the inline action bar
(apply/discard/diff).

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Forward new event types through Core → JSON-RPC

**Files:**
- Modify: `internal/core/core.go`

- [ ] **Step 1: Read existing translator**

В `internal/core/core.go` найти функцию `onEvent` около line 348-369 в `AgentRun` и аналогичную в `SessionMessage` около line 674-690.

Текущий generic mapping:
```go
notify("agent/event", map[string]any{
    "step":            ev.Step,
    "type":            string(ev.Stream.Kind),
    "content":         ev.Stream.Content,
    "tool_call_id":    ev.Stream.ToolCallID,
    "tool_call_name":  ev.Stream.ToolCallName,
    "tool_call_index": ev.Stream.ToolCallIndex,
    "args_delta":      ev.Stream.ArgsDelta,
})
```

Этот же payload подойдёт для всех новых kinds — кроме `pending_ops`, где `Content` это JSON и хочется embed parsed object.

- [ ] **Step 2: Add special-case for pending_ops in AgentRun.onEvent**

Перед общей веткой `notify("agent/event", ...)` добавить:

```go
if ev.Stream.Kind == llm.StreamEventPendingOps {
	var data any
	if err := json.Unmarshal([]byte(ev.Stream.Content), &data); err == nil {
		notify("agent/event", map[string]any{
			"step": ev.Step,
			"type": "pending_ops",
			"data": data,
		})
		return
	}
	// Fall through to generic if unmarshal fails (defensive).
}
```

- [ ] **Step 3: Mirror in SessionMessage.onEvent**

Сделать то же самое в SessionMessage около line 674-690.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/core/core.go
git commit -m "feat(core): forward new agent streaming events as agent/event notifications

tool_call_completed, step_done, recoverable_error flow through the
existing generic translator. pending_ops gets special handling: the
JSON-encoded payload from the agent is parsed and embedded as a data
sub-object in the notification, so clients don't have to double-decode.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Add Server.Request + Client request handler + permission/request wiring

This is the largest single task — three intertwined pieces. Implement in order.

**Files:**
- Modify: `internal/jsonrpc/server.go`
- Modify: `internal/jsonrpc/client.go`
- Create: `internal/core/permissions.go`
- Modify: `internal/core/rpc_handler.go`
- Modify: `internal/core/core.go`
- Modify: `internal/agent/agent.go`

### 7a. Server.Request

- [ ] **Step 1: Add server-side pending state**

В `internal/jsonrpc/server.go` расширить struct `Server`:

```go
type Server struct {
	h Handler
	r *Reader
	w *Writer

	// Server-initiated request state.
	pMu     sync.Mutex
	nextID  int
	pending map[string]chan clientReply
}

type clientReply struct {
	result json.RawMessage
	err    error
}
```

Добавить `import "sync"` если его нет.

- [ ] **Step 2: Modify Serve to handle responses to server-initiated requests**

В `Serve` цикле перед существующей обработкой нужно распознавать входящие responses (id есть, method отсутствует — это ответ на наш Server.Request).

Добавить парсинг входящего payload как `wireMsg` (структуру скопировать из `client.go`, чтобы оба файла её имели — или вынести в общий `types.go`):

```go
type wireMsg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}
```

В Serve цикле, ПЕРЕД существующей обработкой через `parsePayload`:

```go
var probe wireMsg
if json.Unmarshal(msg, &probe) == nil && probe.Method == "" && len(probe.ID) > 0 && string(probe.ID) != "null" {
	// Response to a server-initiated request.
	id := strings.Trim(string(probe.ID), `"`)
	s.pMu.Lock()
	ch, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	s.pMu.Unlock()
	if ok {
		if probe.Error != nil {
			ch <- clientReply{err: &RPCError{Code: probe.Error.Code, Message: probe.Error.Message, Data: probe.Error.Data}}
		} else {
			ch <- clientReply{result: probe.Result}
		}
		continue
	}
	// Stale or unknown response — ignore.
}
```

Добавить `import "strings"` если его нет.

- [ ] **Step 3: Implement Server.Request method**

В том же файле, после `Notify`:

```go
// Request sends a server-initiated JSON-RPC request and waits for the client's response.
// Safe to call concurrently with Serve.
func (s *Server) Request(ctx context.Context, method string, params any, result any) error {
	if s == nil {
		return fmt.Errorf("jsonrpc: server is nil")
	}
	s.pMu.Lock()
	s.nextID++
	id := fmt.Sprintf("srv-%d", s.nextID)
	if s.pending == nil {
		s.pending = make(map[string]chan clientReply)
	}
	ch := make(chan clientReply, 1)
	s.pending[id] = ch
	s.pMu.Unlock()

	removePending := func() {
		s.pMu.Lock()
		delete(s.pending, id)
		s.pMu.Unlock()
	}

	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			removePending()
			return fmt.Errorf("request marshal params: %w", err)
		}
		paramsRaw = b
	}

	idJSON, _ := json.Marshal(id)
	req := Request{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
		Params:  paramsRaw,
	}
	if err := s.w.WriteMessage(req); err != nil {
		removePending()
		return err
	}

	select {
	case <-ctx.Done():
		removePending()
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if result != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, result)
		}
		return nil
	}
}
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean build.

### 7b. Client.SetRequestHandler

- [ ] **Step 5: Extend Client struct**

В `internal/jsonrpc/client.go` добавить поле:

```go
type Client struct {
	// ... existing ...
	onRequest func(ctx context.Context, method string, params json.RawMessage) (any, error)
}
```

- [ ] **Step 6: Add SetRequestHandler**

После `SetNotificationHandler`:

```go
// SetRequestHandler registers a function called for server-initiated requests
// (server → client). The function is called from the read goroutine in a new
// goroutine per request; returning result/err produces the JSON-RPC response.
func (c *Client) SetRequestHandler(fn func(ctx context.Context, method string, params json.RawMessage) (any, error)) {
	c.pMu.Lock()
	c.onRequest = fn
	c.pMu.Unlock()
}
```

- [ ] **Step 7: Modify readLoop to dispatch requests vs notifications**

В `readLoop` (около line 94-103) текущая ветка:

```go
// Notification: has method and absent/null id.
if msg.Method != "" && (len(msg.ID) == 0 || string(msg.ID) == "null") {
	// ... call onNotify ...
	continue
}
```

Перед ней добавить ветку для server-initiated requests:

```go
// Server-initiated request: has method AND non-null id.
if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
	c.pMu.Lock()
	fn := c.onRequest
	c.pMu.Unlock()
	if fn == nil {
		// No handler registered — return method-not-found.
		c.wMu.Lock()
		_ = c.w.WriteMessage(Response{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &Error{Code: -32601, Message: "Method not found"},
		})
		c.wMu.Unlock()
		continue
	}
	go func(id json.RawMessage, method string, params json.RawMessage) {
		ctx := context.Background()
		result, err := fn(ctx, method, params)
		c.wMu.Lock()
		defer c.wMu.Unlock()
		if err != nil {
			_ = c.w.WriteMessage(Response{
				JSONRPC: "2.0",
				ID:      id,
				Error:   &Error{Code: -32603, Message: err.Error()},
			})
			return
		}
		var resultRaw json.RawMessage
		if result != nil {
			b, _ := json.Marshal(result)
			resultRaw = b
		}
		_ = c.w.WriteMessage(Response{
			JSONRPC: "2.0",
			ID:      id,
			Result:  resultRaw,
		})
	}(msg.ID, msg.Method, msg.Params)
	continue
}
```

- [ ] **Step 8: Build**

Run: `go build ./...`
Expected: clean build.

### 7c. PermissionRequester wiring

- [ ] **Step 9: Create internal/core/permissions.go**

```go
package core

import "context"

// PermissionRequester asks the connected client (TUI/IDE) for interactive
// consent before running a sensitive tool (e.g. exec.run).
// Returns Approved=true if permitted, false otherwise.
type PermissionRequester interface {
	RequestPermission(ctx context.Context, req PermissionRequest) (PermissionResponse, error)
}

type PermissionRequest struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	Reason      string `json:"reason,omitempty"`
}

type PermissionResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// rpcPermissionRequester routes PermissionRequest through a server-initiated request function.
type rpcPermissionRequester struct {
	requestFn func(ctx context.Context, method string, params any, result any) error
}

func (r *rpcPermissionRequester) RequestPermission(ctx context.Context, req PermissionRequest) (PermissionResponse, error) {
	var resp PermissionResponse
	if r.requestFn == nil {
		return PermissionResponse{Approved: false}, nil
	}
	if err := r.requestFn(ctx, "permission/request", req, &resp); err != nil {
		return PermissionResponse{Approved: false}, err
	}
	return resp, nil
}
```

- [ ] **Step 10: Add SetRequester to RPCHandler**

В `internal/core/rpc_handler.go` после `SetNotifier` (line 30):

```go
// SetRequester attaches a request function so that core.AgentRun can issue
// server-initiated requests (e.g. permission/request) to the client.
func (h *RPCHandler) SetRequester(fn func(ctx context.Context, method string, params any, result any) error) {
	h.requester = fn
}
```

Добавить поле `requester` в struct `RPCHandler`.

- [ ] **Step 11: Pass requester into AgentRunParams**

В `internal/core/core.go::AgentRunParams` добавить:

```go
type AgentRunParams struct {
	// ... existing ...
	PermissionRequester PermissionRequester `json:"-"`
}
```

В `internal/core/rpc_handler.go::Handle` для `agent.run` (line 62-75) и `session.message`:

```go
case "agent.run":
	var p AgentRunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if h.notifier != nil {
		p.OnEvent = func(method string, params any) {
			_ = h.notifier(method, params)
		}
	}
	if h.requester != nil {
		p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
	}
	return h.core.AgentRun(ctx, p)
```

То же для `session.message`.

- [ ] **Step 12: Pass requester from Core.AgentRun into agent.Options**

В `internal/core/core.go::AgentRun` (около line 397-423 где конструируется `agent.Options`) добавить ещё одно поле:

```go
ag, err := agent.New(customOpts.llmClient, c.validator, c.tools, agent.Options{
	// ... existing ...
	PermissionRequester: convertRequester(params.PermissionRequester),
})
```

Где `convertRequester` — функция-адаптер, конвертирующая `core.PermissionRequester` в `agent.PermissionRequester` (это разные interfaces одной структуры из-за import boundary):

```go
func convertRequester(r PermissionRequester) agent.PermissionRequester {
	if r == nil {
		return nil
	}
	return &agentRequesterAdapter{inner: r}
}

type agentRequesterAdapter struct {
	inner PermissionRequester
}

func (a *agentRequesterAdapter) RequestPermission(ctx context.Context, req agent.PermissionRequest) (agent.PermissionResponse, error) {
	innerReq := PermissionRequest{Tool: req.Tool, Description: req.Description, Reason: req.Reason}
	innerResp, err := a.inner.RequestPermission(ctx, innerReq)
	return agent.PermissionResponse{Approved: innerResp.Approved, Reason: innerResp.Reason}, err
}
```

- [ ] **Step 13: Define mirror interface in agent package**

В `internal/agent/agent.go` (или новом `internal/agent/permissions.go`):

```go
// PermissionRequester is the agent's view of an interactive consent provider.
// Mirrors core.PermissionRequester (same shape, different package to avoid import cycle).
type PermissionRequester interface {
	RequestPermission(ctx context.Context, req PermissionRequest) (PermissionResponse, error)
}

type PermissionRequest struct {
	Tool        string
	Description string
	Reason      string
}

type PermissionResponse struct {
	Approved bool
	Reason   string
}
```

Добавить в `Options`:

```go
type Options struct {
	// ... existing ...

	// PermissionRequester, if non-nil, is consulted before running exec.run/bash
	// instead of (or before) the static AllowExec gate. nil → fall back to gate.
	PermissionRequester PermissionRequester
}
```

- [ ] **Step 14: Use PermissionRequester in agent's exec.run consent path**

Найти в `internal/agent/agent.go` место где обрабатывается consent для `bash`/`exec.run` (поиск по `AllowExec` и `bash`). Около line 555-560 уже есть проверка `name == "bash"`.

Перед существующей логикой консента добавить:

```go
if name == "bash" && a.opts.PermissionRequester != nil {
	cmdPreview := ""
	if len(step.Tool.Input) > 0 {
		cmdPreview = string(step.Tool.Input)
		if len(cmdPreview) > 200 {
			cmdPreview = cmdPreview[:200] + "..."
		}
	}
	resp, err := a.opts.PermissionRequester.RequestPermission(ctx, PermissionRequest{
		Tool:        "bash",
		Description: cmdPreview,
	})
	if err != nil || !resp.Approved {
		// Treat as denial: synthesize TOOL_DENIED-shaped error and continue loop.
		// Use the same code path that the static AllowExec=false branch uses
		// (search for "TOOL_DENIED" in this file to find the exact pattern).
		// ... wire into existing denial path ...
	}
}
```

**Подгонка под код:** найти как сейчас формируется TOOL_DENIED ответ когда `AllowExec=false` и `name=="bash"` — и переиспользовать тот же return path.

- [ ] **Step 15: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 16: Commit**

```bash
git add internal/jsonrpc/server.go internal/jsonrpc/client.go internal/core/permissions.go internal/core/rpc_handler.go internal/core/core.go internal/agent/agent.go
git commit -m "feat: server-initiated requests + permission/request RPC

Three coupled additions:
  - jsonrpc.Server.Request: server can issue requests with srv-N IDs
    and await client responses. Mirrors Client.Call.
  - jsonrpc.Client.SetRequestHandler: client dispatches incoming
    server-initiated requests to a registered handler.
  - core.PermissionRequester + rpcPermissionRequester: agent loop
    consults the connected client before exec.run/bash. Falls back to
    static AllowExec gate when no requester wired in (CLI mode).

The agent package mirrors PermissionRequester locally to avoid import
cycle; an adapter in core converts between the two.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Test audit pass

This task picks up all the testing work skipped in Tasks 1-7. Per user preference, tests are written **after** all functionality is in place.

**Files (all Modify):**
- `internal/agent/agent_test.go`
- `internal/jsonrpc/server_test.go`
- `internal/jsonrpc/client_test.go`
- `internal/core/rpc_handler_test.go`

- [ ] **Step 1: Run existing tests to find regressions from Tasks 1-7**

Run: `go test ./... -count=1`

Expected: некоторые тесты могут падать из-за изменения сигнатур (`Options.PermissionRequester`, новые fields в structs). Это ожидаемо.

Зафиксировать список упавших тестов — будем чинить в этом же task.

- [ ] **Step 2: Fix existing broken tests**

Проходимся по списку из Step 1, чиним. Типичные правки:
- Новые поля в `agent.Options{}`: добавить нулевые значения если тесты использовали именованные literals
- Изменения в payload `agent/event` (если делали refactor): обновить assertion'ы
- Если `protocol_version` в expected response — обновить с 2 на 3 (после Task 9, но если в этом task сразу — обновить)

Run after fixes: `go test ./... -count=1`
Expected: все тесты зелёные (или только наши новые ниже добавятся).

- [ ] **Step 3: Add tests for tool_call_completed**

В `internal/agent/agent_test.go` добавить:

```go
func TestAgent_EmitsToolCallCompletedEvent(t *testing.T) {
	mockLLM := newMockLLMSequence(t, []string{
		`{"type":"tool_call","tool":{"name":"read","input":{"path":"go.mod"}}}`,
		`{"type":"final","final":{"patches":[]}}`,
	})
	runner := newTestRunner(t)
	v := newTestValidator(t)

	var events []llm.StreamEvent
	ag, err := agent.New(mockLLM, v, runner, agent.Options{
		MaxSteps: 5,
		Apply:    false,
		OnEvent: func(ev agent.AgentEvent) {
			events = append(events, ev.Stream)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, err = ag.Run(context.Background(), nil, "read go.mod")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var found bool
	for _, ev := range events {
		if ev.Kind == llm.StreamEventToolCallCompleted && ev.ToolCallName == "read" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool_call_completed event for fs.read, got events: %+v", events)
	}
}
```

(Если `newMockLLMSequence`, `newTestRunner`, `newTestValidator` не существуют как helpers — посмотрите паттерны в существующих тестах файла `agent_test.go` и переиспользуйте их шаблон. Скорее всего, фикстуры уже есть под другими именами.)

- [ ] **Step 4: Add tests for step_done**

```go
func TestAgent_EmitsStepDoneAfterEachStep(t *testing.T) {
	mockLLM := newMockLLMSequence(t, []string{
		`{"type":"tool_call","tool":{"name":"read","input":{"path":"go.mod"}}}`,
		`{"type":"final","final":{"patches":[]}}`,
	})
	runner := newTestRunner(t)
	v := newTestValidator(t)

	var stepDoneCount int
	ag, _ := agent.New(mockLLM, v, runner, agent.Options{
		MaxSteps: 5,
		Apply:    false,
		OnEvent: func(ev agent.AgentEvent) {
			if ev.Stream.Kind == llm.StreamEventStepDone {
				stepDoneCount++
			}
		},
	})
	_, _, err := ag.Run(context.Background(), nil, "test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if stepDoneCount != 2 {
		t.Fatalf("expected 2 step_done events, got %d", stepDoneCount)
	}
}
```

- [ ] **Step 5: Add tests for recoverable_error**

```go
func TestAgent_EmitsRecoverableErrorOnStaleContent(t *testing.T) {
	mockLLM := newMockLLMSequence(t, []string{
		`{"type":"final","final":{"patches":[{
			"op":"file.search_replace",
			"path":"go.mod",
			"file_hash":"deadbeef",
			"search":"nonexistent text",
			"replace":"new"
		}]}}`,
		`{"type":"final","final":{"patches":[]}}`,
	})
	runner := newTestRunner(t)
	v := newTestValidator(t)

	var recoverableCount int
	ag, _ := agent.New(mockLLM, v, runner, agent.Options{
		MaxSteps: 5,
		Apply:    false,
		OnEvent: func(ev agent.AgentEvent) {
			if ev.Stream.Kind == llm.StreamEventRecoverableError {
				recoverableCount++
			}
		},
	})
	_, _, _ = ag.Run(context.Background(), nil, "test")
	if recoverableCount == 0 {
		t.Fatalf("expected at least one recoverable_error event")
	}
}
```

- [ ] **Step 6: Add tests for pending_ops**

```go
func TestAgent_EmitsPendingOpsOnDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hash := sha256Hex([]byte("hello\n"))

	mockLLM := newMockLLMSequence(t, []string{
		fmt.Sprintf(`{"type":"final","final":{"patches":[{
			"op":"file.search_replace",
			"path":"hello.txt",
			"file_hash":"%s",
			"search":"hello",
			"replace":"hi"
		}]}}`, hash),
	})
	runner := newTestRunnerInDir(t, tmpDir)
	v := newTestValidator(t)

	var pendingPayloads []string
	ag, _ := agent.New(mockLLM, v, runner, agent.Options{
		MaxSteps: 5,
		Apply:    false,
		OnEvent: func(ev agent.AgentEvent) {
			if ev.Stream.Kind == llm.StreamEventPendingOps {
				pendingPayloads = append(pendingPayloads, ev.Stream.Content)
			}
		},
	})
	_, _, err := ag.Run(context.Background(), nil, "rename hello to hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(pendingPayloads) != 1 {
		t.Fatalf("expected exactly 1 pending_ops event, got %d", len(pendingPayloads))
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(pendingPayloads[0]), &p); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	for _, key := range []string{"ops", "diff"} {
		if _, ok := p[key]; !ok {
			t.Errorf("payload missing %q key: %s", key, pendingPayloads[0])
		}
	}
}
```

- [ ] **Step 7: Add Server.Request round-trip test**

В `internal/jsonrpc/server_test.go`:

```go
func TestServer_RequestRoundTrip(t *testing.T) {
	cToS, sFromC := io.Pipe()
	sToC, cFromS := io.Pipe()

	srv := jsonrpc.NewServer(noopHandler{}, sFromC, sToC)
	cli := jsonrpc.NewClient(cFromS, cToS)

	cli.SetRequestHandler(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != "permission/request" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return map[string]any{"approved": true}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go srv.Serve(ctx)

	var result map[string]any
	err := srv.Request(ctx, "permission/request", map[string]any{"tool": "exec.run"}, &result)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if approved, ok := result["approved"].(bool); !ok || !approved {
		t.Fatalf("expected approved=true, got %+v", result)
	}
}

type noopHandler struct{}

func (noopHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	return nil, nil
}
```

- [ ] **Step 8: Add integration test for permission/request through RPC handler**

В `internal/core/rpc_handler_test.go` (если есть подходящий test setup) добавить тест где `SetRequester` мокается и проверяется что agent.run с bash-вызовом действительно вызывает `permission/request`.

Если тестовый scaffolding слишком сложный — отметить как DONE_WITH_CONCERNS и зафиксировать что нужен отдельный e2e тест в `tests/integration`.

- [ ] **Step 9: Run all tests with race detector**

Run: `go test -race ./internal/agent ./internal/core ./internal/jsonrpc ./internal/llm -count=1`
Expected: All pass.

- [ ] **Step 10: Run full test suite**

Run: `go test ./... -count=1`
Expected: All pass.

- [ ] **Step 11: Commit**

```bash
git add internal/agent/agent_test.go internal/jsonrpc/server_test.go internal/jsonrpc/client_test.go internal/core/rpc_handler_test.go
git commit -m "test: cover Phase 0 streaming events and permission/request

Adds tests for the four new agent-level streaming events, the new
server-initiated Server.Request round-trip, and the permission/request
RPC integration. Also fixes any tests that broke from Options struct
field additions in earlier Phase 0 commits.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Bump ProtocolVersion + document new methods/events

**Files:**
- Modify: `internal/protocol/version.go`
- Modify: `docs/PROTOCOL.md`

- [ ] **Step 1: Bump ProtocolVersion**

`internal/protocol/version.go` line 9:

```go
// ProtocolVersion is the version of JSON-RPC methods / schemas.
// v3: added agent-level streaming events (tool_call_completed, step_done,
//     pending_ops, recoverable_error) and bidirectional permission/request.
ProtocolVersion = 3
```

- [ ] **Step 2: Update tests that hardcoded protocol_version=2**

Поиск: `grep -r "protocol_version.*2" internal/`. Заменить на 3 где обнаружено.

Run: `go test ./... -count=1`
Expected: All pass.

- [ ] **Step 3: Update docs/PROTOCOL.md**

Добавить (или расширить) раздел:

```markdown
## Streaming events (server → client notifications)

While `agent.run` or `session.message` is in progress, the core may emit
JSON-RPC notifications to the client (no `id` field, one-way).

### `agent/event`

Generic envelope:

| Field | Type | Description |
|---|---|---|
| `step` | int | Current agent loop step number |
| `type` | string | One of the kinds below |
| `content` | string | Type-specific payload (string), used for most kinds |
| `data` | object | Type-specific structured payload, only for `pending_ops` |
| `tool_call_id`, `tool_call_name`, `tool_call_index`, `args_delta` | optional | Set for tool-call-related kinds |

### Event types

| `type` | Emitted when | Notable fields |
|---|---|---|
| `message_delta` | LLM streamed a token of assistant text | `content` |
| `tool_call_start` | LLM declared intent to call a tool | `tool_call_name`, `tool_call_id` |
| `tool_call_delta` | More argument bytes for in-progress call | `args_delta` |
| `tool_call_completed` | Agent loop finished `tools.Call` | `tool_call_name`, `tool_call_id`, `content` (truncated preview to 256 bytes) |
| `step_done` | End of one agent loop iteration | `content` ∈ {tool_call, final, invalid, compaction, final_retry} |
| `pending_ops` | Agent finalized patches (dry-run or pre-apply) | `data` = `{ops: [...], diff: "...", applied: bool}` |
| `recoverable_error` | StaleContent / AmbiguousMatch / schema invalid; loop will retry | `content` (short message) |
| `done` | LLM stream ended | (full assembled response in agent state) |
| `error` | LLM-stream-level error (different from `recoverable_error`) | `content` |

### `exec/output_chunk`

Streamed during `bash` (alias for `exec.run`) tool execution.

| Field | Type | Description |
|---|---|---|
| `step` | int | Current agent loop step |
| `chunk` | string | Raw stdout/stderr chunk |

## Server-initiated requests

Requests where the server initiates and the client must respond. Use JSON-
RPC `id` fields to correlate. Server uses `srv-N`-prefixed IDs to avoid
collision with client-initiated requests.

### `permission/request`

Asks the user (via TUI/IDE) for interactive consent before running a
sensitive tool.

Params:

```json
{"tool": "bash", "description": "go test ./...", "reason": "to verify the fix"}
```

Expected response (`result`):

```json
{"approved": true, "reason": "ok"}
```

If no client request handler is registered (`Client.SetRequestHandler` not
called), the client returns method-not-found and the server falls back to
the static permission gate (config `exec.confirm` / `--allow-exec`).
```

- [ ] **Step 4: Build + test**

Run: `go build ./... && go test ./... -count=1`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/version.go docs/PROTOCOL.md
git commit -m "docs(protocol): bump ProtocolVersion to 3, document streaming events

ProtocolVersion 2 → 3 captures the new agent-level event types and the
permission/request bidirectional method added in this phase.

Documents agent/event envelope, all event type semantics, and the
permission/request request-response contract in docs/PROTOCOL.md.

Closes TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase 0 Completion Criteria

After all 9 tasks:

- [ ] `go test ./... -count=1` зелёный на Linux и Windows
- [ ] `go test -race ./internal/jsonrpc ./internal/core ./internal/agent -count=10` стабильно проходит
- [ ] `orchestra apply --via-core "..."` продолжает работать (smoke check)
- [ ] Mock-клиент или test ловит минимум один `tool_call_completed`, `step_done` и `pending_ops` через `agent/event`
- [ ] `permission/request` round-trip покрыт unit-тестом
- [ ] `docs/PROTOCOL.md` отражает все новые типы и метод

После закрытия фазы — обновить память (`project_status.md` → добавить «TUI Phase 0 готова»), приступать к написанию плана Фазы 1 (TUI скелет).

---

## Notes for the implementing engineer

1. **Имена переменных в agent.go.** Я указывал номера строк по состоянию на коммит `8900267`. Если файл изменился — ищите по контексту: `a.tools.Call(`, `a.tools.FSApplyOps(`, `protocol.StaleContent`. Имена локальных переменных (`stepIdx`, `stepReason`, `resolvedOps`) подбирайте под существующий стиль файла.

2. **Import cycle между agent и core.** Дублирование `PermissionRequester` interface в обоих пакетах — нормальный Go-паттерн для этого случая. Адаптер в `core.go` конвертирует между ними.

3. **Не изменяйте существующее поведение `exec/output_chunk`.** Этот канал уже использует TUI-альтернативы и тесты `--via-core`. Новые события идут только через `agent/event`.

4. **Generic translator в core.go (lines ~348-369).** Если решите оставить полностью generic mapping без специальной ветки для `pending_ops` — клиенты получат `content` как JSON-строку. Это допустимо, но придётся документировать в `docs/PROTOCOL.md` отдельно. Чище — embed parsed object, как в Task 6 step 2.

5. **Тесты в Task 8.** Mock-LLM хелперы в `agent_test.go` посмотреть в существующих тестах файла. Не делайте новый отдельный пакет mock-LLM ради этого. Если фикстуры действительно отсутствуют — создайте локальные, минимально-инвазивные.

6. **Workflow rule.** Tasks 1-7 — только реализация + `go build ./...`. Без тестов. Если builds падает — чините. Если existing test упал — пометьте список и продолжайте. Все тесты собираются в Task 8.
