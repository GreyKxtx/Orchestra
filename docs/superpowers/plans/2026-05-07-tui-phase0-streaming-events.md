# TUI Phase 0 — Streaming Events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Расширить существующий streaming-протокол `agent/event` четырьмя недостающими типами событий (`tool_call_completed`, `step_done`, `pending_ops`, `recoverable_error`) и добавить bidirectional метод `permission/request` ↔ `permission/response`. Подготовить ядро к подключению будущего TUI без касания UI-слоя.

**Architecture:** Большая часть инфраструктуры уже есть: `jsonrpc.Server.Notify`, `Client.SetNotificationHandler`, `Agent.Options.OnEvent`, `Core.AgentRunParams.OnEvent`. Текущий `OnEvent` эмитит LLM-уровневые события (`message_delta`, `tool_call_start/delta`, `done`, `error`) и `exec/output_chunk`. Добавляем агент-уровневые события: после успешного `tools.Call`, после `final` с патчами, после каждого шага, при recoverable resolve-ошибках. `permission/request` — новый JSON-RPC метод где **сервер делает запрос клиенту** (использует уже существующий `Server.Notify`-канал, но с `id` для ответа); это требует расширения `jsonrpc.Server` методом `Request` и обработчиком ответов клиента.

**Tech Stack:** Go (existing). Никаких новых зависимостей.

**Reference:** [`docs/superpowers/specs/2026-05-07-tui-design.md`](../specs/2026-05-07-tui-design.md), секции «Streaming-события в JSON-RPC».

---

## File Structure

| Файл | Создаётся / Изменяется | Ответственность |
|---|---|---|
| `internal/agent/agent.go` | Modify | Эмиссия `tool_call_completed`, `step_done`, `pending_ops`, `recoverable_error` через `Options.OnEvent` |
| `internal/agent/types.go` | Modify | Расширение `llm.StreamEventKind`-эквивалентов или новые константы для агент-уровневых типов |
| `internal/llm/stream.go` | Modify | Добавить `StreamEventKind` константы для агент-уровневых событий |
| `internal/agent/agent_test.go` | Modify | Tests на новые события (счётчик через mock OnEvent) |
| `internal/core/core.go` | Modify | Маппинг новых `StreamEventKind` → JSON payload в `AgentRun`/`SessionMessage` (lines ~348-369 и ~674-690) |
| `internal/core/rpc_handler.go` | Modify | Обработка ответа `permission/response` (входящий запрос от клиента после нашего request) |
| `internal/jsonrpc/server.go` | Modify | Добавить `Server.Request(method, params, result)` — server-initiated request с ожиданием ответа |
| `internal/jsonrpc/server_test.go` | Modify | Tests на `Server.Request` |
| `internal/jsonrpc/client.go` | Modify | Клиент должен отвечать на server-initiated requests; добавить `SetRequestHandler` |
| `internal/jsonrpc/client_test.go` | Modify | Tests на server-initiated request handling |
| `internal/core/permissions.go` | Create | `PermissionRequester` interface — мост от agent loop к JSON-RPC `permission/request` |
| `internal/agent/permissions.go` | Modify | Использование `PermissionRequester` вместо текущей `QuestionAsker` для `exec.run` consent (опционально) |
| `docs/PROTOCOL.md` | Modify | Документация всех новых типов событий + `permission/request` |
| `internal/protocol/version.go` | Modify | Бамп `ProtocolVersion` 2 → 3 |

---

## Task 1: Add agent-level StreamEventKind constants

**Files:**
- Modify: `internal/llm/stream.go`

- [ ] **Step 1: Add new event kind constants**

В `internal/llm/stream.go` после `StreamEventExecOutput` (line 34):

```go
const (
	// ... existing constants ...

	// StreamEventToolCallCompleted is emitted by the agent loop after a tool.Call returns.
	// Content holds a short result preview (truncated). ToolCallID/ToolCallName identify the call.
	StreamEventToolCallCompleted StreamEventKind = "tool_call_completed"

	// StreamEventStepDone is emitted at the end of each agent loop iteration.
	// Content holds the reason: "tool_call", "final", "invalid", "compaction".
	StreamEventStepDone StreamEventKind = "step_done"

	// StreamEventPendingOps is emitted when the agent produces final patches (dry-run or pre-apply).
	// Content holds a JSON-encoded {ops: [...], diff: "..."} payload.
	StreamEventPendingOps StreamEventKind = "pending_ops"

	// StreamEventRecoverableError is emitted when a non-fatal error (StaleContent, AmbiguousMatch,
	// schema validation failure) occurs and the loop will retry. Content holds a short message.
	StreamEventRecoverableError StreamEventKind = "recoverable_error"
)
```

- [ ] **Step 2: Build and verify no breakage**

Run: `go build ./...`
Expected: builds clean, no warnings.

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

## Task 2: Emit `tool_call_completed` after Runner.Call

**Files:**
- Modify: `internal/agent/agent.go` (around line 586 where `a.tools.Call` is invoked)
- Modify: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

В `internal/agent/agent_test.go` добавить тест (после существующих тестов `TestAgent_Run`):

```go
func TestAgent_EmitsToolCallCompletedEvent(t *testing.T) {
	// Mock LLM: first response is a tool call (fs.read), second is final (no patches).
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

	// Expect at least one tool_call_completed event with name=read.
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

(Если `newMockLLMSequence` / `newTestRunner` / `newTestValidator` не существуют как helpers — посмотрите на существующие тесты в этом файле и используйте те же фикстуры. Если их нет, создайте по образцу `agent_test.go` соседних тестов.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run TestAgent_EmitsToolCallCompletedEvent -v`
Expected: FAIL — событие не эмитируется.

- [ ] **Step 3: Implement emission**

В `internal/agent/agent.go` после строки `out, err := a.tools.Call(callCtx, name, step.Tool.Input)` (около line 586) добавить:

```go
// Emit tool_call_completed event for streaming clients.
if a.opts.OnEvent != nil {
	preview := ""
	if out != nil {
		previewBytes := out
		const maxPreview = 256
		if len(previewBytes) > maxPreview {
			previewBytes = append(previewBytes[:maxPreview:maxPreview], []byte("...(truncated)")...)
		}
		preview = string(previewBytes)
	}
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:         llm.StreamEventToolCallCompleted,
		ToolCallName: name,
		ToolCallID:   step.Tool.CallID,
		Content:      preview,
		ErrorMessage: errMsg,
	}})
}
```

(Замените `stepIdx` и `step.Tool.CallID` на реальные имена переменных в этой функции — посмотрите контекст вокруг line 586. Если в `llm.StreamEvent` нет поля `ErrorMessage`, передайте сообщение через `Content` с префиксом `error: `.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent -run TestAgent_EmitsToolCallCompletedEvent -v`
Expected: PASS.

- [ ] **Step 5: Run full agent tests to verify no regression**

Run: `go test ./internal/agent -v`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): emit tool_call_completed event after Runner.Call

Streaming clients (TUI) need to know when a tool call finished, not just
when the LLM declared its intent (existing tool_call_start). Emits via
Options.OnEvent with truncated result preview.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Emit `step_done` and `recoverable_error` events

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test for step_done**

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

	// Expect 2 step_done events (one per loop iteration).
	if stepDoneCount != 2 {
		t.Fatalf("expected 2 step_done events, got %d", stepDoneCount)
	}
}
```

- [ ] **Step 2: Write the failing test for recoverable_error**

```go
func TestAgent_EmitsRecoverableErrorOnStaleContent(t *testing.T) {
	// Mock returns a final with patches that won't resolve (stale hash).
	// Then on second attempt returns no patches.
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

- [ ] **Step 3: Run both tests to verify they fail**

Run: `go test ./internal/agent -run "TestAgent_EmitsStepDone|TestAgent_EmitsRecoverableError" -v`
Expected: FAIL.

- [ ] **Step 4: Implement step_done emission**

В `internal/agent/agent.go` найдите конец каждой итерации цикла `Run` (после `nextStep` обработки). В конце каждой итерации (после tool dispatch, после invalid retry, после final-resolve loop) перед `continue` или перед следующей итерацией добавить:

```go
if a.opts.OnEvent != nil {
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventStepDone,
		Content: stepReason, // "tool_call" | "final" | "invalid" | "compaction"
	}})
}
```

(Если в коде сейчас нет переменной `stepReason` — введите её и устанавливайте в каждой ветке: после успешного tool call = `"tool_call"`, после `final` без ошибок = `"final"`, после schema validation failure = `"invalid"`, после compaction = `"compaction"`.)

- [ ] **Step 5: Implement recoverable_error emission**

Около line 668-669 (`StaleContent`/`AmbiguousMatch` ветка) добавить перед `continue`:

```go
if a.opts.OnEvent != nil {
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventRecoverableError,
		Content: fmt.Sprintf("%s: %s", pe.Code, pe.Message),
	}})
}
```

То же самое для schema validation failures (где сейчас увеличивается счётчик `MaxInvalidRetries`) — найдите соответствующее место в `Run`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/agent -run "TestAgent_EmitsStepDone|TestAgent_EmitsRecoverableError" -v`
Expected: PASS.

- [ ] **Step 7: Full regression check**

Run: `go test ./internal/agent ./internal/core -v`
Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): emit step_done and recoverable_error streaming events

step_done fires at the end of every loop iteration with the reason.
recoverable_error fires for StaleContent/AmbiguousMatch/schema invalid
before the loop retries — gives streaming clients (TUI) visibility into
what the agent is recovering from.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Emit `pending_ops` event with diff

**Files:**
- Modify: `internal/agent/agent.go` (around line 658-665 — `applyReq`/`FSApplyOps`)
- Modify: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAgent_EmitsPendingOpsOnDryRun(t *testing.T) {
	// Use a real fs.search_replace patch that resolves to a real file.
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
		Apply:    false, // dry-run
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
	// Payload should be JSON containing "ops" and "diff" keys.
	var p map[string]any
	if err := json.Unmarshal([]byte(pendingPayloads[0]), &p); err != nil {
		t.Fatalf("payload not JSON: %v\npayload: %s", err, pendingPayloads[0])
	}
	if _, ok := p["ops"]; !ok {
		t.Fatalf("payload missing 'ops' key: %s", pendingPayloads[0])
	}
	if _, ok := p["diff"]; !ok {
		t.Fatalf("payload missing 'diff' key: %s", pendingPayloads[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run TestAgent_EmitsPendingOpsOnDryRun -v`
Expected: FAIL.

- [ ] **Step 3: Implement emission**

В `internal/agent/agent.go` после успешного `a.tools.FSApplyOps` (около line 665), но перед возвратом `Result`, добавить:

```go
if a.opts.OnEvent != nil && resp != nil {
	payload := map[string]any{
		"ops":     resolvedOps,         // []ops.AnyOp; имя переменной может отличаться — посмотрите контекст
		"diff":    resp.UnifiedDiff,    // если поле называется иначе — поправьте
		"applied": params.Apply,         // или a.opts.Apply
	}
	payloadJSON, _ := json.Marshal(payload)
	a.opts.OnEvent(AgentEvent{Step: stepIdx, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventPendingOps,
		Content: string(payloadJSON),
	}})
}
```

Точные имена полей берите из `tools.FSApplyOpsResponse` (`internal/tools/`) и из переменной с резолвнутыми ops в этой функции.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent -run TestAgent_EmitsPendingOpsOnDryRun -v`
Expected: PASS.

- [ ] **Step 5: Verify no regression in apply tests**

Run: `go test ./internal/agent ./internal/cli -v`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): emit pending_ops event with ops + diff payload

After agent emits 'final' with patches and they resolve+apply (dry-run
or write), emit a pending_ops streaming event carrying the resolved ops
and unified diff. TUI uses this to render the inline action bar
(apply/discard/diff).

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Wire new event types into Core → JSON-RPC

**Files:**
- Modify: `internal/core/core.go` (around line 348-369 in `AgentRun` and ~674-690 in `SessionMessage`)
- Modify: `internal/core/rpc_handler_test.go`

The existing `onEvent` translator in `core.go` already maps `agent.AgentEvent` → JSON payload generically using `ev.Stream.Kind`. New event kinds should "just work" — but let's verify and add explicit handling if the payload needs different shape (e.g. `pending_ops` carries JSON in `Content` and we want to embed it as a sub-object).

- [ ] **Step 1: Read the existing translator**

Read `internal/core/core.go` lines 348-369. Confirm that the generic mapping (`type: string(ev.Stream.Kind), content: ev.Stream.Content, ...`) is sufficient for `tool_call_completed`, `step_done`, `recoverable_error`. For `pending_ops`, the `Content` is a JSON string — clients will have to do double-decode unless we unmarshal here.

- [ ] **Step 2: Write the failing test**

Add to `internal/core/rpc_handler_test.go`:

```go
func TestAgentRun_NotifiesNewEventTypes(t *testing.T) {
	c := newTestCore(t) // existing helper
	h := core.NewRPCHandler(c)

	var notifiedEvents []map[string]any
	h.SetNotifier(func(method string, params any) error {
		if method != "agent/event" {
			return nil
		}
		// Re-encode through JSON to capture serialized form.
		b, _ := json.Marshal(params)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		notifiedEvents = append(notifiedEvents, m)
		return nil
	})

	// Initialize first.
	_, err := h.Handle(context.Background(), "initialize", initParamsJSON(t))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Run a simple agent call (mock LLM in newTestCore setup).
	_, err = h.Handle(context.Background(), "agent.run", agentRunParamsJSON(t, "test query"))
	if err != nil {
		t.Fatalf("agent.run: %v", err)
	}

	// Assert at least one event of each new type was notified.
	wantTypes := map[string]bool{
		"tool_call_completed": false,
		"step_done":           false,
	}
	for _, ev := range notifiedEvents {
		if t, ok := ev["type"].(string); ok {
			if _, ok := wantTypes[t]; ok {
				wantTypes[t] = true
			}
		}
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("expected at least one %s event, got none", typ)
		}
	}
}
```

(Если `newTestCore`, `initParamsJSON`, `agentRunParamsJSON` не существуют — посмотрите как другие тесты в этом файле строят `Core` для тестов и переиспользуйте паттерн.)

- [ ] **Step 3: Run test to verify it fails or passes**

Run: `go test ./internal/core -run TestAgentRun_NotifiesNewEventTypes -v`

Если тест **проходит** сразу — generic mapping работает, переходите к step 5 (адаптация для `pending_ops`).
Если падает — внести правки в `internal/core/core.go::AgentRun.onEvent` (lines ~348-369): расширить switch на новые `StreamEventKind` если требуется специфичный payload.

- [ ] **Step 4: Adapt pending_ops payload to embed parsed JSON**

В `internal/core/core.go::AgentRun.onEvent` (около line 360 — общий `notify("agent/event", ...)` блок) перед общей веткой добавить:

```go
if ev.Stream.Kind == llm.StreamEventPendingOps {
	// Content is JSON; embed parsed object instead of string.
	var data any
	if err := json.Unmarshal([]byte(ev.Stream.Content), &data); err == nil {
		notify("agent/event", map[string]any{
			"step": ev.Step,
			"type": "pending_ops",
			"data": data,
		})
		return
	}
	// Fall through to generic if unmarshal fails.
}
```

Сделать то же самое в `SessionMessage` (около line 674-690).

- [ ] **Step 5: Run all core tests**

Run: `go test ./internal/core -v`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/core.go internal/core/rpc_handler_test.go
git commit -m "feat(core): forward new agent streaming events as agent/event notifications

tool_call_completed, step_done, recoverable_error flow through the
existing generic translator. pending_ops gets special handling: the
JSON-encoded payload from the agent is parsed and embedded as a
sub-object in the notification, so clients don't have to double-decode.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Add `Server.Request` for server-initiated bidirectional calls

The current `jsonrpc.Server.Notify` is one-way (no response). For `permission/request` we need server→client→server: server asks, client answers, server gets the answer. JSON-RPC 2.0 supports this naturally — the server sends a request with an `id`, the client returns a response with the same `id`.

The complication: our current `Server` doesn't keep a pending-response map (only the `Client` does). We need symmetric pending tracking on the `Server` side.

**Files:**
- Modify: `internal/jsonrpc/server.go`
- Modify: `internal/jsonrpc/client.go`
- Modify: `internal/jsonrpc/server_test.go`
- Modify: `internal/jsonrpc/client_test.go`

- [ ] **Step 1: Write the failing test (server side)**

В `internal/jsonrpc/server_test.go`:

```go
func TestServer_RequestRoundTrip(t *testing.T) {
	// Pipe server↔client.
	cToS, sFromC := io.Pipe()
	sToC, cFromS := io.Pipe()

	// Server with a no-op handler (we won't drive its Serve loop with regular requests).
	srv := jsonrpc.NewServer(noopHandler{}, sFromC, sToC)

	// Client side: handle incoming requests by echoing back result {"approved":true}.
	cli := jsonrpc.NewClient(cFromS, cToS)
	cli.SetRequestHandler(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != "permission/request" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return map[string]any{"approved": true}, nil
	})

	// Drive server in goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go srv.Serve(ctx)

	// Server makes a request to client.
	var result map[string]any
	err := srv.Request(ctx, "permission/request", map[string]any{"tool": "exec.run"}, &result)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if approved, ok := result["approved"].(bool); !ok || !approved {
		t.Fatalf("expected approved=true, got %+v", result)
	}
}
```

(`noopHandler` — `Handler` который возвращает nil/error; сделайте локальный для теста.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/jsonrpc -run TestServer_RequestRoundTrip -v`
Expected: FAIL — `Server.Request` и `Client.SetRequestHandler` не существуют.

- [ ] **Step 3: Implement Server.Request**

В `internal/jsonrpc/server.go` после `Notify`:

```go
// Server-initiated request state.
// Add to Server struct:
//   pMu       sync.Mutex
//   nextID    int
//   pending   map[string]chan clientReply
//   readyOnce sync.Once

type clientReply struct {
	result json.RawMessage
	err    error
}

// Request sends a server-initiated JSON-RPC request and waits for the client's response.
// Safe to call concurrently with Serve.
func (s *Server) Request(ctx context.Context, method string, params any, result any) error {
	if s == nil {
		return fmt.Errorf("jsonrpc: server is nil")
	}
	s.pMu.Lock()
	s.nextID++
	id := s.nextID
	idStr := fmt.Sprintf("srv-%d", id)
	if s.pending == nil {
		s.pending = make(map[string]chan clientReply)
	}
	ch := make(chan clientReply, 1)
	s.pending[idStr] = ch
	s.pMu.Unlock()

	removePending := func() {
		s.pMu.Lock()
		delete(s.pending, idStr)
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

	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%q", idStr)),
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

Дополнительно в `Serve` цикле обработки сообщений добавить распознавание ответов клиента: если входящее сообщение — это response (есть `id`, нет `method`), и id начинается с `"srv-"` — найти pending канал и доставить ответ. Потребуется правка `parsePayload` чтобы он не отбрасывал такие сообщения.

Альтернатива (проще): добавить в `Serve` отдельную ветку для случая когда `req.Method == ""` и `req.ID != nil` — это входящий response на наш request:

```go
// Inside Serve, after parsing the message but before dispatching to Handler:
var probe wireMsg
if json.Unmarshal(msg, &probe) == nil && probe.Method == "" && len(probe.ID) > 0 {
	// This is a response to our server-initiated request.
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
}
```

(`wireMsg` уже определён в `client.go` — переиспользуйте или продублируйте локально.)

- [ ] **Step 4: Implement Client.SetRequestHandler**

В `internal/jsonrpc/client.go`:

```go
// Add to Client struct:
//   onRequest func(ctx context.Context, method string, params json.RawMessage) (any, error)

func (c *Client) SetRequestHandler(fn func(ctx context.Context, method string, params json.RawMessage) (any, error)) {
	c.pMu.Lock()
	c.onRequest = fn
	c.pMu.Unlock()
}
```

В `readLoop` (около line 84) расширить условие распознавания: если `msg.Method != ""` И `len(msg.ID) > 0 && string(msg.ID) != "null"` — это server-initiated request, не notification. Запускаем `onRequest` в горутине и шлём response обратно:

```go
if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
	c.pMu.Lock()
	fn := c.onRequest
	c.pMu.Unlock()
	if fn == nil {
		// No handler — return method-not-found error.
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
		_ = c.w.WriteMessage(Response{
			JSONRPC: "2.0",
			ID:      id,
			Result:  marshalAny(result),
		})
	}(msg.ID, msg.Method, msg.Params)
	continue
}
```

Где `marshalAny` — локальный helper:

```go
func marshalAny(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
```

- [ ] **Step 5: Run round-trip test**

Run: `go test ./internal/jsonrpc -run TestServer_RequestRoundTrip -v`
Expected: PASS.

- [ ] **Step 6: Run full jsonrpc tests**

Run: `go test ./internal/jsonrpc -race -v`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add internal/jsonrpc/server.go internal/jsonrpc/client.go internal/jsonrpc/server_test.go internal/jsonrpc/client_test.go
git commit -m "feat(jsonrpc): add Server.Request for server-initiated bidirectional calls

Mirrors Client.Call: server can issue a JSON-RPC request to the client
and await the response. Required for permission/request flow where the
core needs interactive consent from the TUI before running exec.run.

Server.Request uses srv-prefixed IDs to avoid collision with client-
initiated requests. Client gains SetRequestHandler for handling these
server-initiated calls.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Wire `permission/request` through Core → Agent

**Files:**
- Create: `internal/core/permissions.go`
- Modify: `internal/core/rpc_handler.go`
- Modify: `internal/agent/agent.go` (consent path for `exec.run`)

- [ ] **Step 1: Define PermissionRequester interface**

Create `internal/core/permissions.go`:

```go
package core

import "context"

// PermissionRequester asks the connected client (TUI/IDE) for interactive
// consent before running a sensitive tool (e.g. exec.run).
// Returns true if approved, false otherwise.
// If the transport doesn't support interactive prompts (no client connected),
// implementations should return false (deny by default).
type PermissionRequester interface {
	RequestPermission(ctx context.Context, req PermissionRequest) (PermissionResponse, error)
}

type PermissionRequest struct {
	Tool        string `json:"tool"`         // e.g. "exec.run"
	Description string `json:"description"`  // e.g. "go test ./..."
	Reason      string `json:"reason,omitempty"` // why the model wants this
}

type PermissionResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"` // optional user-provided reason
}
```

- [ ] **Step 2: Implement RPC-backed requester**

В том же файле:

```go
// rpcPermissionRequester routes PermissionRequest through Server.Request.
type rpcPermissionRequester struct {
	notifier Notifier // existing interface in rpc_handler.go
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

- [ ] **Step 3: Hook into RPCHandler**

В `internal/core/rpc_handler.go` (около `SetNotifier` — line 30):

```go
// SetRequester attaches a request function so that core.AgentRun can issue
// server-initiated requests (e.g. permission/request) to the client.
func (h *RPCHandler) SetRequester(fn func(ctx context.Context, method string, params any, result any) error) {
	h.requester = fn
}
```

Добавить поле `requester` в struct `RPCHandler`. В команде `agent.run` (line 62-75) и `session.message` (line ~110) перед `h.core.AgentRun`/`SessionMessage` сконструировать `PermissionRequester`:

```go
case "agent.run":
	var p AgentRunParams
	// ... existing ...
	if h.requester != nil {
		p.PermissionRequester = &rpcPermissionRequester{requestFn: h.requester}
	}
	return h.core.AgentRun(ctx, p)
```

- [ ] **Step 4: Add PermissionRequester to AgentRunParams**

В `internal/core/core.go`:

```go
type AgentRunParams struct {
	// ... existing fields ...
	PermissionRequester PermissionRequester `json:"-"`
}
```

В `Core.AgentRun` пробросить в `agent.Options.PermissionRequester` (поле потребуется добавить в Options).

В `internal/agent/agent.go::Options` добавить:

```go
// PermissionRequester, if non-nil, is consulted before running exec.run instead of
// (or in addition to) the static AllowExec gate. nil → fall back to existing gate.
PermissionRequester core.PermissionRequester  // или собственный interface чтобы избежать import cycle
```

**Важно про import cycle:** `internal/agent` не должен импортировать `internal/core`. Решение: продублируйте `PermissionRequester` interface в `internal/agent` (Go interfaces are structural — `*rpcPermissionRequester` будет удовлетворять обоим).

- [ ] **Step 5: Use PermissionRequester in agent's exec.run consent path**

В `internal/agent/agent.go` найти место где сейчас проверяется `opts.AllowExec` для `exec.run` (или эквивалент `bash`). Перед статическим gate'ом, если `opts.PermissionRequester != nil`, спрашиваем динамически:

```go
if name == "bash" && a.opts.PermissionRequester != nil {
	resp, err := a.opts.PermissionRequester.RequestPermission(ctx, agent.PermissionRequest{
		Tool:        "bash",
		Description: extractCommandPreview(step.Tool.Input),
	})
	if err == nil && resp.Approved {
		// proceed
	} else {
		// deny: synthesize TOOL_DENIED error so loop reports back to model
		return &Result{...}, nil // wire into existing denial path
	}
}
```

Точное место — где сейчас обрабатывается consent для `bash`/`exec.run` (поищите по `AllowExec` в agent.go).

- [ ] **Step 6: Write integration test**

В `internal/core/rpc_handler_test.go`:

```go
func TestAgentRun_RoutesPermissionRequestThroughRequester(t *testing.T) {
	c := newTestCore(t)
	h := core.NewRPCHandler(c)

	var seenRequest core.PermissionRequest
	h.SetRequester(func(ctx context.Context, method string, params any, result any) error {
		if method != "permission/request" {
			return fmt.Errorf("unexpected method: %s", method)
		}
		// Capture and approve.
		b, _ := json.Marshal(params)
		_ = json.Unmarshal(b, &seenRequest)
		resp := result.(*core.PermissionResponse)
		resp.Approved = true
		return nil
	})

	// Initialize, then agent.run with a query that triggers exec.run.
	// (Use a mock LLM that emits a bash tool call.)
	_, _ = h.Handle(context.Background(), "initialize", initParamsJSON(t))
	_, err := h.Handle(context.Background(), "agent.run", agentRunParamsJSONWithBash(t))
	if err != nil {
		t.Fatalf("agent.run: %v", err)
	}

	if seenRequest.Tool != "bash" {
		t.Fatalf("expected permission request for bash, got %+v", seenRequest)
	}
}
```

- [ ] **Step 7: Run all tests**

Run: `go test ./internal/agent ./internal/core ./internal/jsonrpc -race -v`
Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add internal/core/permissions.go internal/core/rpc_handler.go internal/core/core.go internal/agent/agent.go internal/core/rpc_handler_test.go
git commit -m "feat(core,agent): route exec.run consent through permission/request RPC

Adds PermissionRequester interface (in core, mirrored in agent to avoid
import cycle). When set, the agent loop calls it before exec.run instead
of falling back to the static AllowExec gate.

The RPC handler wires it to Server.Request via SetRequester, so the
TUI/IDE can prompt the user interactively. If no requester is wired in
(direct CLI mode without TUI), behavior falls back to the static gate.

Part of TUI Phase 0 (streaming events).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Bump ProtocolVersion and document new methods/events

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

- [ ] **Step 2: Update docs/PROTOCOL.md**

Добавить раздел «Streaming events» (если его нет) или расширить существующий. Минимальное содержимое:

```markdown
## Streaming events (server → client notifications)

While `agent.run` is in progress, the core may emit `agent/event` and
`exec/output_chunk` notifications to the client. These are JSON-RPC 2.0
notifications (no `id`), one-way.

### `agent/event`

Generic envelope:

| Field | Type | Description |
|---|---|---|
| `step` | int | Current agent loop step number |
| `type` | string | One of the kinds below |
| `content` | string | Type-specific payload (string) |
| `data` | object | Type-specific structured payload (only for `pending_ops`) |
| `tool_call_id`, `tool_call_name`, `tool_call_index`, `args_delta` | optional | Set for tool-call-related types |

### Event types

| `type` | Emitted when | Notable fields |
|---|---|---|
| `message_delta` | LLM streamed a token of assistant text | `content` |
| `tool_call_start` | LLM declared intent to call a tool | `tool_call_name`, `tool_call_id` |
| `tool_call_delta` | More argument bytes for in-progress call | `args_delta` |
| `tool_call_completed` | Agent loop finished `tools.Call` | `tool_call_name`, `content` (truncated preview), `error_message` (if failed) |
| `step_done` | End of one agent loop iteration | `content` ∈ {tool_call, final, invalid, compaction} |
| `pending_ops` | Agent emitted final patches (dry-run or pre-apply) | `data` = `{ops: [...], diff: "...", applied: bool}` |
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
RPC `id` fields to correlate. Server uses `srv-N` IDs to avoid collision.

### `permission/request`

Asks the user (via the TUI/IDE) for interactive consent.

Params:

```json
{"tool": "bash", "description": "go test ./...", "reason": "to verify the fix"}
```

Expected response (`result`):

```json
{"approved": true, "reason": "ok"}
```

If no client request handler is registered, the server falls back to the
static permission gate (config `exec.confirm` / `--allow-exec`).
```

- [ ] **Step 3: Build everything to verify protocol_version bump compiles**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 4: Run smoke test for initialize protocol_version mismatch**

Run: `go test ./internal/core -run TestInitialize -v`
Expected: PASS (existing tests should still match version 3 if they're not version-pinned; if they are, update them to expect 3).

Если есть тесты с захардкоженным `protocol_version: 2` — обновите их на 3 в этом же коммите.

- [ ] **Step 5: Run full regression**

Run: `go test ./... -count=1`
Expected: All pass on Linux/macOS. On Windows: `go test ./...` (без `-race`).

- [ ] **Step 6: Commit**

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

After all 8 tasks:

- [ ] `go test ./... -count=1` зелёный на Linux и Windows
- [ ] `go test -race ./internal/jsonrpc ./internal/core ./internal/agent -count=10` стабильно проходит
- [ ] `orchestra apply --via-core "..."` продолжает работать (smoke check без LLM или с mock)
- [ ] Mock-клиент (или ручной заглушка-скрипт) может подключиться к `orchestra core`, отправить `agent.run` с mock-LLM-сценарием, и получить минимум один `tool_call_completed`, `step_done` и `pending_ops` через `agent/event` notifications
- [ ] `permission/request` round-trip покрыт unit-тестом
- [ ] `docs/PROTOCOL.md` отражает все новые типы и метод

После закрытия фазы — обновить память (project_status.md → добавить «TUI Phase 0 готова») и приступать к написанию плана Фазы 1 (TUI скелет).

---

## Notes for the implementing engineer

1. **Имена переменных в agent.go.** Я указывал номера строк по состоянию на коммит `8900267`. Если файл изменился — ищите по контексту: `a.tools.Call(`, `a.tools.FSApplyOps(`, `protocol.StaleContent`. Имена локальных переменных (`stepIdx`, `stepReason`, `resolvedOps`) подбирайте под существующий стиль файла.

2. **Import cycle между agent и core.** Если попытаетесь импортировать `core.PermissionRequester` из `internal/agent` — будет цикл. Дублирование interface в обоих пакетах — нормальный Go-паттерн для этого случая.

3. **Не изменяйте существующее поведение `exec/output_chunk`.** Этот канал уже использует TUI-альтернативы и тесты `--via-core`. Новые события идут только через `agent/event`.

4. **Generic translator в core.go (lines ~348-369).** Если решите оставить полностью generic mapping без специальной ветки для `pending_ops` — клиенты получат `content` как JSON-строку. Это допустимо, но придётся документировать в `docs/PROTOCOL.md` отдельно. Чище — embed parsed object, как в Task 5 step 4.

5. **Тесты integration-уровня.** Mock-LLM хелперы в `agent_test.go` не такой уж сложный. Если их нет в нужном виде — создайте локально по паттерну существующих. Не делайте новый отдельный пакет mock-LLM ради этого.
