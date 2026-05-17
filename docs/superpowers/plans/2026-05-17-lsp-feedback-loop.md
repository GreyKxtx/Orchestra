# LSP Diagnostics Feedback Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the agent calls `write` or `edit` as a tool call and the response contains LSP diagnostics with `severity:"error"`, automatically inject a user-role hint message into conversation history so the model fixes compile errors in the next step.

**Architecture:** `FSWrite`/`FSEdit` already call `lspManager.SyncAndDiagnose()` and return `diagnostics` in their JSON response — but the agent loop discards that field. We add (1) a test seam `ForceDiagnosticsForTest` to `RunnerOptions` so tests can inject fake diagnostics without an LSP server, (2) a pure helper `extractLSPErrors(out json.RawMessage) string` in `agent.go` that parses the tool response for `severity:"error"` items, and (3) a three-line injection after the tool-result message is appended to history. System prompt gets a one-paragraph update about the diagnostic field.

**Tech Stack:** Go, `encoding/json`, `internal/lsp.ToolDiagnostic`, `internal/llm.Message`

---

## File Map

| Action | File | Purpose |
|--------|------|---------|
| Modify | `internal/tools/runner.go` | Add `ForceDiagnosticsForTest []lsp.ToolDiagnostic` to `RunnerOptions` and `Runner` |
| Modify | `internal/tools/write.go` | Append forced diags after LSP block (test seam) |
| Modify | `internal/tools/edit.go` | Append forced diags after LSP block (test seam) |
| Modify | `internal/agent/agent.go` | Add `extractLSPErrors()` + inject hint after write/edit tool calls |
| Modify | `internal/prompt/files/build.txt` | Tell model about `diagnostics` field and what to do |
| Test   | `internal/tools/write_test.go` | Verify `ForceDiagnosticsForTest` surfaces in response |
| Test   | `internal/agent/agent_test.go` | Unit tests for `extractLSPErrors` + integration test |

---

### Task 1: Test seam — `ForceDiagnosticsForTest` in tools.Runner

**Files:**
- Modify: `internal/tools/runner.go` (struct + RunnerOptions, lines ~57–88 and ~150–165)
- Modify: `internal/tools/write.go` (after LSP block, lines ~95–107)
- Modify: `internal/tools/edit.go` (after LSP block, lines ~101–106)
- Test: `internal/tools/write_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/tools/write_test.go`:

```go
func TestFSWrite_ForceDiagnosticsForTest(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{
		ForceDiagnosticsForTest: []lsp.ToolDiagnostic{
			{StartLine: 2, StartCol: 1, Severity: "error", Message: "undefined: Bar"},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	resp, err := r.FSWrite(context.Background(), FSWriteRequest{
		Path:         "a.go",
		Content:      "package main\n",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}
	if len(resp.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(resp.Diagnostics))
	}
	if resp.Diagnostics[0].Severity != "error" || resp.Diagnostics[0].Message != "undefined: Bar" {
		t.Errorf("unexpected diagnostic: %+v", resp.Diagnostics[0])
	}
}
```

Add import to the write_test.go import block:
```go
"github.com/orchestra/orchestra/internal/lsp"
```

- [ ] **Step 2: Run the test to verify it fails**

```
cd D:\CursorProjects\Orchestra
go test ./internal/tools/ -run TestFSWrite_ForceDiagnosticsForTest -v
```

Expected: FAIL — `unknown field "ForceDiagnosticsForTest"` or similar compile error.

- [ ] **Step 3: Add `ForceDiagnosticsForTest` to `RunnerOptions` and `Runner`**

In `internal/tools/runner.go`, in the `Runner` struct (after `dryRun bool`, around line 64):
```go
// forceDiagnosticsForTest is appended to every write/edit diagnostic response.
// Only used in tests — nil in production.
forceDiagnosticsForTest []lsp.ToolDiagnostic
```

In `RunnerOptions` (after `DryRun bool`, around line 87):
```go
// ForceDiagnosticsForTest, if non-nil, is appended to every FSWrite/FSEdit
// diagnostic response. Only for use in tests.
ForceDiagnosticsForTest []lsp.ToolDiagnostic
```

In `NewRunner`, in the return struct literal (after `dryRun: opts.DryRun,`, around line 163):
```go
forceDiagnosticsForTest: opts.ForceDiagnosticsForTest,
```

- [ ] **Step 4: Apply forced diags in `write.go`**

In `internal/tools/write.go`, replace the LSP block (lines ~95–100):

```go
var diags []lsp.ToolDiagnostic
if r.lspManager != nil && !r.lspManager.IsEmpty() {
    if _, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path); err == nil {
        diags = r.lspManager.SyncAndDiagnose(ctx, relSlash, req.Content)
    }
}
diags = append(diags, r.forceDiagnosticsForTest...)
```

- [ ] **Step 5: Apply forced diags in `edit.go`**

In `internal/tools/edit.go`, replace the LSP block (lines ~101–104):

```go
var diags []lsp.ToolDiagnostic
if r.lspManager != nil && !r.lspManager.IsEmpty() {
    diags = r.lspManager.SyncAndDiagnose(ctx, relSlash, content)
}
diags = append(diags, r.forceDiagnosticsForTest...)
```

- [ ] **Step 6: Run the test to verify it passes**

```
go test ./internal/tools/ -run TestFSWrite_ForceDiagnosticsForTest -v
```

Expected: PASS.

- [ ] **Step 7: Run the full tools test suite**

```
go test ./internal/tools/ -v -count=1 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```
git add internal/tools/runner.go internal/tools/write.go internal/tools/edit.go internal/tools/write_test.go
git commit -m "test(tools): add ForceDiagnosticsForTest seam to Runner for LSP feedback tests"
```

---

### Task 2: `extractLSPErrors` helper — unit tests and implementation

**Files:**
- Modify: `internal/agent/agent.go` (add function after `formatApplyErrorCompact`, around line 1355)
- Modify: `internal/agent/agent_test.go` (add unit tests)

- [ ] **Step 1: Write the failing unit tests**

Add to `internal/agent/agent_test.go` (near the top, after the existing helper types):

```go
func TestExtractLSPErrors_Nil(t *testing.T) {
	if got := extractLSPErrors(nil); got != "" {
		t.Errorf("nil input: expected empty string, got %q", got)
	}
}

func TestExtractLSPErrors_EmptyObject(t *testing.T) {
	if got := extractLSPErrors(json.RawMessage(`{}`)); got != "" {
		t.Errorf("empty object: expected empty string, got %q", got)
	}
}

func TestExtractLSPErrors_NoDiagnostics(t *testing.T) {
	out := json.RawMessage(`{"path":"a.go","file_hash":"abc123"}`)
	if got := extractLSPErrors(out); got != "" {
		t.Errorf("no diagnostics field: expected empty string, got %q", got)
	}
}

func TestExtractLSPErrors_OnlyWarnings(t *testing.T) {
	out := json.RawMessage(`{"diagnostics":[{"severity":"warning","message":"unused import","start_line":1,"start_col":1}]}`)
	if got := extractLSPErrors(out); got != "" {
		t.Errorf("warning-only: expected empty string, got %q", got)
	}
}

func TestExtractLSPErrors_SingleError(t *testing.T) {
	out := json.RawMessage(`{"diagnostics":[{"severity":"error","message":"undefined: Foo","start_line":3,"start_col":5}]}`)
	got := extractLSPErrors(out)
	if got == "" {
		t.Fatal("expected non-empty hint for error diagnostic")
	}
	if !strings.Contains(got, "line 3:5") {
		t.Errorf("expected 'line 3:5' in hint, got %q", got)
	}
	if !strings.Contains(got, "undefined: Foo") {
		t.Errorf("expected error message in hint, got %q", got)
	}
	if !strings.Contains(got, "LSP_ERRORS") {
		t.Errorf("expected 'LSP_ERRORS' prefix in hint, got %q", got)
	}
}

func TestExtractLSPErrors_MixedSeverity(t *testing.T) {
	out := json.RawMessage(`{"diagnostics":[
		{"severity":"warning","message":"unused var","start_line":1,"start_col":1},
		{"severity":"error","message":"type mismatch","start_line":5,"start_col":3},
		{"severity":"information","message":"consider renaming","start_line":7,"start_col":1}
	]}`)
	got := extractLSPErrors(out)
	if !strings.Contains(got, "type mismatch") {
		t.Errorf("expected error message in hint, got %q", got)
	}
	if strings.Contains(got, "unused var") {
		t.Errorf("warning should not appear in hint, got %q", got)
	}
	if strings.Contains(got, "consider renaming") {
		t.Errorf("information should not appear in hint, got %q", got)
	}
}

func TestExtractLSPErrors_MultipleErrors(t *testing.T) {
	out := json.RawMessage(`{"diagnostics":[
		{"severity":"error","message":"first error","start_line":2,"start_col":1},
		{"severity":"error","message":"second error","start_line":4,"start_col":3}
	]}`)
	got := extractLSPErrors(out)
	if !strings.Contains(got, "first error") {
		t.Errorf("expected first error in hint, got %q", got)
	}
	if !strings.Contains(got, "second error") {
		t.Errorf("expected second error in hint, got %q", got)
	}
}

func TestExtractLSPErrors_InvalidJSON(t *testing.T) {
	if got := extractLSPErrors(json.RawMessage(`not json at all`)); got != "" {
		t.Errorf("invalid JSON: expected empty string, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```
go test ./internal/agent/ -run TestExtractLSPErrors -v
```

Expected: FAIL — `extractLSPErrors undefined`.

- [ ] **Step 3: Implement `extractLSPErrors` in `agent.go`**

In `internal/agent/agent.go`, add after the `formatApplyErrorCompact` function (around line 1355):

```go
// extractLSPErrors parses a write/edit tool response JSON and returns a
// user-facing hint if diagnostics with severity "error" are present.
// Returns "" if there are no errors (warnings and info are silently ignored).
func extractLSPErrors(out json.RawMessage) string {
	if len(out) == 0 {
		return ""
	}
	var resp struct {
		Diagnostics []struct {
			Severity  string `json:"severity"`
			Message   string `json:"message"`
			StartLine int    `json:"start_line"`
			StartCol  int    `json:"start_col"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || len(resp.Diagnostics) == 0 {
		return ""
	}
	var errs []string
	for _, d := range resp.Diagnostics {
		if d.Severity == "error" {
			errs = append(errs, fmt.Sprintf("  line %d:%d: %s", d.StartLine, d.StartCol, d.Message))
		}
	}
	if len(errs) == 0 {
		return ""
	}
	return "LSP_ERRORS: файл записан с ошибками компиляции:\n" +
		strings.Join(errs, "\n") +
		"\nИсправь ошибки и примени изменения снова."
}
```

- [ ] **Step 4: Run the unit tests to verify they pass**

```
go test ./internal/agent/ -run TestExtractLSPErrors -v
```

Expected: all PASS.

- [ ] **Step 5: Run the full agent test suite**

```
go test ./internal/agent/ -v -count=1 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): add extractLSPErrors helper for LSP diagnostic hint injection"
```

---

### Task 3: Inject LSP hint into agent loop + integration test

**Files:**
- Modify: `internal/agent/agent.go` (lines 782–784, after tool result appended to history)
- Modify: `internal/agent/agent_test.go` (add integration test + helper LLM type)

- [ ] **Step 1: Write the failing integration test**

Add to `internal/agent/agent_test.go`:

First add a new import for lsp and tools:
```go
// Already imported: tools, llm, cache, schema — add:
"github.com/orchestra/orchestra/internal/lsp"
```

Then add the helper LLM type and test:

```go
// lspHintLLM scripts two steps:
//  1. Call the "write" tool.
//  2. Verify a user message with "LSP_ERRORS" was injected, then return final.
type lspHintLLM struct {
	step       int
	fileHash   string
	hintSeen   bool
}

func (l *lspHintLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func (l *lspHintLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	switch l.step {
	case 0:
		l.step++
		// Ask the agent to call write on "a.go".
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   "call_write_1",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name: "write",
					Arguments: llm.ToolArguments([]byte(
						`{"path":"a.go","content":"package main\n","file_hash":"` + l.fileHash + `"}`,
					)),
				},
			}},
		}}, nil
	default:
		l.step++
		// Check that a user message with LSP_ERRORS was injected after the tool result.
		for _, m := range req.Messages {
			if m.Role == llm.RoleUser && strings.Contains(m.Content, "LSP_ERRORS") {
				l.hintSeen = true
				break
			}
		}
		return &llm.CompleteResponse{Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: `{"type":"final","final":{"patches":[]}}`,
		}}, nil
	}
}

func TestAgent_Run_LSPErrors_HintInjected(t *testing.T) {
	root := t.TempDir()
	// Pre-create the file with known content so we have its hash.
	content := "package main\n"
	h := cache.ComputeSHA256([]byte(content))
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{
		ForceDiagnosticsForTest: []lsp.ToolDiagnostic{
			{StartLine: 1, StartCol: 1, Severity: "error", Message: "undefined: Bar"},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	mockLLM := &lspHintLLM{fileHash: h}

	ag, err := New(mockLLM, v, tr, Options{
		MaxSteps: 10,
		Apply:    true,
		Backup:   false,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = ag.Run(context.Background(), nil, "write a.go with package main")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !mockLLM.hintSeen {
		t.Error("expected LSP_ERRORS hint in LLM messages after write tool call")
	}
}
```

- [ ] **Step 2: Run the integration test to verify it fails**

```
go test ./internal/agent/ -run TestAgent_Run_LSPErrors_HintInjected -v
```

Expected: FAIL — `hintSeen == false` (hint not injected yet).

- [ ] **Step 3: Add the injection to the agent loop in `agent.go`**

In `internal/agent/agent.go`, after line 782 (after the tool result is appended to history), add the injection block. The current code at ~line 778:

```go
			history = append(history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    string(out),
			})
			// Record call for future dedup checks.
			cb.RecordSuccessfulCall(name, step.Tool.Input)
```

Replace with:

```go
			history = append(history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    string(out),
			})
			// Inject LSP error hint so the model fixes compile errors in the next step.
			if name == "write" || name == "edit" {
				if hint := extractLSPErrors(out); hint != "" {
					a.logf("lsp_hint name=%s injecting diagnostic hint", name)
					history = append(history, llm.Message{
						Role:    llm.RoleUser,
						Content: hint,
					})
				}
			}
			// Record call for future dedup checks.
			cb.RecordSuccessfulCall(name, step.Tool.Input)
```

- [ ] **Step 4: Run the integration test to verify it passes**

```
go test ./internal/agent/ -run TestAgent_Run_LSPErrors_HintInjected -v
```

Expected: PASS.

- [ ] **Step 5: Run the full agent test suite**

```
go test ./internal/agent/ -v -count=1 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): inject LSP error hint into history after write/edit tool calls"
```

---

### Task 4: System prompt update

**Files:**
- Modify: `internal/prompt/files/build.txt`

No dedicated test — the system prompt is a plain text file. Verify by running a build and smoke-checking the file renders correctly.

- [ ] **Step 1: Update `build.txt` to document LSP diagnostics**

In `internal/prompt/files/build.txt`, append the following section at the end (after the last line `НЕЛЬЗЯ: применить изменение через edit/write И дублировать его же в patches — это вызовет StaleContent ошибку.`):

```
ДИАГНОСТИКА LSP:
Ответ инструментов edit и write может содержать поле "diagnostics" — массив диагностик от языкового сервера.
Если в массиве есть элементы с "severity":"error", файл записан с ошибками компиляции.
Ты ДОЛЖЕН прочитать ошибки из сообщения LSP_ERRORS (появится автоматически) и исправить код.
Предупреждения ("severity":"warning") — информационные, игнорируй их если задача не требует их устранения.
```

- [ ] **Step 2: Verify the build compiles**

```
go build -o orchestra_tmp ./cmd/orchestra
```

Expected: builds without errors (build.txt is embedded at compile time).

Remove the temporary binary:
```
del orchestra_tmp
```

- [ ] **Step 3: Run the full test suite**

```
go test ./...
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```
git add internal/prompt/files/build.txt
git commit -m "docs(prompt): tell model to fix LSP_ERRORS after write/edit tool calls"
```

---

## Self-Review

**1. Spec coverage:**
- ✅ Diagnostics with `severity:"error"` trigger a user-role hint — Task 3
- ✅ Warning/info severities are silently ignored — `extractLSPErrors` filter, tested in Task 2
- ✅ Test seam so tests run without a real LSP server — Task 1
- ✅ System prompt documents the new behavior — Task 4
- ✅ Existing dry-run path unaffected — `write.go`/`edit.go` early returns before the `diags` block

**2. Placeholder scan:** No TBD, TODO, or vague instructions. All steps include complete code.

**3. Type consistency:**
- `lsp.ToolDiagnostic` — same type used in `RunnerOptions.ForceDiagnosticsForTest`, `FSWriteResponse.Diagnostics`, `FSEditResponse.Diagnostics`
- `extractLSPErrors(out json.RawMessage) string` — used in Task 2 unit tests and Task 3 injection, same signature
- `lspHintLLM.hintSeen bool` — set in `Complete`, checked in test assertion
