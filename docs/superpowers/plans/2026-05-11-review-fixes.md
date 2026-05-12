# Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Закрыть 5 проблем, найденных в код-ревью: тест на невалидный JSON, обратная совместимость темы, логика AppendToolArgsDelta, EnableThinking как *bool, порядок fuzzy-результатов в палитре.

**Architecture:** Каждое исправление независимо; порядок выполнения совпадает с порядком задач. Никаких новых файлов — только правки в существующих.

**Tech Stack:** Go 1.22+, bubbletea, lipgloss, charmbracelet/bubbles, sahilm/fuzzy

---

### Task 1: Тест на невалидный/malformed JSON в agent

Старый `TestAgent_Run_InvalidJSON_Retries` удалён. Нужно убедиться, что malformed JSON (не plain prose) тоже завершает агент без ошибки, но с нулевым числом патчей — а не в retry-цикле.

**Files:**
- Modify: `internal/agent/agent_test.go`

- [ ] **Step 1: Добавить тест `TestAgent_Run_MalformedJSON_IsFinal`**

Вставить после существующего `TestAgent_Run_PlainTextIsFinal` (строка ~295):

```go
func TestAgent_Run_MalformedJSON_IsFinal(t *testing.T) {
	// Malformed JSON that looks intentional but fails schema validation must
	// also terminate the agent loop cleanly (no retry, no error) rather than
	// spinning forever. The same plain-text-as-final contract applies: if the
	// model can't produce valid tool_calls or a PatchSet, the answer is its
	// natural-language output — no file mutations.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	cases := []struct {
		name string
		resp string
	}{
		{"truncated_json", `{"type":"final","final":{`},
		{"bad_schema", `{"not_a_valid_agent_step": true}`},
		{"json_in_prose", `Here is my answer: {"patches": "not_an_array"} done.`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			llm := &scriptedLLM{steps: []string{tc.resp}}
			ag, err := New(llm, v, tr, Options{MaxSteps: 5, MaxInvalidRetries: 2})
			if err != nil {
				t.Fatalf("New agent failed: %v", err)
			}
			_, res, err := ag.Run(context.Background(), nil, "do something")
			if err != nil {
				t.Fatalf("Run returned error on malformed JSON (%s): %v", tc.name, err)
			}
			if res == nil {
				t.Fatalf("expected non-nil result (%s)", tc.name)
			}
			if len(res.Patches) != 0 {
				t.Fatalf("expected zero patches (%s), got %d", tc.name, len(res.Patches))
			}
			after, _ := os.ReadFile(filepath.Join(root, "a.txt"))
			if string(after) != "x" {
				t.Fatalf("file unexpectedly mutated (%s): %q", tc.name, string(after))
			}
		})
	}
}
```

- [ ] **Step 2: Запустить тест и убедиться, что проходит**

```
go test ./internal/agent -run TestAgent_Run_MalformedJSON_IsFinal -v
```

Ожидаемый вывод: `PASS` по всем 3 подтестам.

- [ ] **Step 3: Запустить все тесты пакета**

```
go test ./internal/agent -v
```

Ожидаемый вывод: все тесты `PASS`.

- [ ] **Step 4: Коммит**

```
git add internal/agent/agent_test.go
git commit -m "test(agent): add TestAgent_Run_MalformedJSON_IsFinal — covers truncated/bad-schema JSON"
```

---

### Task 2: Обратная совместимость дефолтной темы

`DefaultTheme` сменился с `orchestra` на `neutral` молча для всех существующих пользователей без `ui.theme` в конфиге. Исправление: вернуть дефолт на `orchestra`; neutral остаётся доступным по имени.

**Files:**
- Modify: `ui/tui/theme/theme.go`

- [ ] **Step 1: Сменить `DefaultTheme` обратно на "orchestra"**

В файле `ui/tui/theme/theme.go` строка 33:

```go
// DefaultTheme is the name of the theme used when nothing is configured or
// the configured theme is unknown.
const DefaultTheme = "orchestra"
```

- [ ] **Step 2: Запустить тесты темы и TUI**

```
go test ./ui/tui/... ./ui/tui/theme/... -v
```

Ожидаемый вывод: все `PASS` (тесты не привязаны к конкретному цвету).

- [ ] **Step 3: Коммит**

```
git add ui/tui/theme/theme.go
git commit -m "fix(theme): restore orchestra as default — neutral stays available by name"
```

---

### Task 3: Исправить `AppendToolArgsDelta` — логика матчинга по ID

Текущий код использует `blocks[i].ID == ""` как дополнительный матч-критерий, что может неверно направить дельту при нескольких одновременных инструментах с пустым ID. Привести к той же логике «первый running», что в `UpdateToolBlock`.

**Files:**
- Modify: `ui/tui/state/session.go`

- [ ] **Step 1: Переписать `AppendToolArgsDelta`**

Заменить тело функции (строки ~127-141):

```go
func (s *Session) AppendToolArgsDelta(id, delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	blocks := s.Messages[s.activeAssistant].ToolBlocks
	// Exact ID match first.
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Status == ToolBlockRunning && blocks[i].ID == id && id != "" {
			blocks[i].ArgsRaw += delta
			return
		}
	}
	// Fallback: first running block — same strategy as UpdateToolBlock.
	// Handles the case where the LLM omits tool IDs on streaming deltas.
	for i := range blocks {
		if blocks[i].Status == ToolBlockRunning {
			blocks[i].ArgsRaw += delta
			return
		}
	}
}
```

- [ ] **Step 2: Запустить тесты state**

```
go test ./ui/tui/state/... -v
```

Ожидаемый вывод: все `PASS`.

- [ ] **Step 3: Коммит**

```
git add ui/tui/state/session.go
git commit -m "fix(state): AppendToolArgsDelta — first-running fallback matches UpdateToolBlock logic"
```

---

### Task 4: `EnableThinking bool` → `*bool` в ModelPreset

`bool` zero-value `false` неотличима от «пользователь явно отключил thinking». `*bool` позволяет хранить три состояния: `nil` (не задано), `true`, `false`.

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Сменить тип поля**

В файле `internal/config/config.go` строка 56:

```go
type ModelPreset struct {
	Provider       string  `yaml:"provider,omitempty"`
	APIBase        string  `yaml:"api_base,omitempty"`
	Temperature    float32 `yaml:"temperature,omitempty"`
	MaxTokens      int     `yaml:"max_tokens,omitempty"`
	NumCtx         int64   `yaml:"num_ctx,omitempty"`
	EnableThinking *bool   `yaml:"enable_thinking,omitempty"`
}
```

- [ ] **Step 2: Найти все места использования `EnableThinking` и обновить**

```
grep -rn "EnableThinking" --include="*.go" .
```

Для каждого места, где поле читается (проверяется как bool), добавить разыменование:

```go
// было:
if preset.EnableThinking { ... }

// стало:
if preset.EnableThinking != nil && *preset.EnableThinking { ... }
```

Для мест, где поле записывается:

```go
// было:
preset.EnableThinking = true

// стало:
v := true
preset.EnableThinking = &v
```

- [ ] **Step 3: Сборка и тесты**

```
go build ./...
go test ./internal/config/... -v
```

Ожидаемый вывод: `build OK`, тесты `PASS`.

- [ ] **Step 4: Коммит**

```
git add internal/config/config.go
git commit -m "fix(config): ModelPreset.EnableThinking *bool — distinguish unset from explicit false"
```

---

### Task 5: Fuzzy-сортировка по релевантности в PaletteModal

При активном фильтре (`filter != ""`) результаты сортируются по индексу объявления, а не по fuzzy-score. Это делает поиск менее полезным. Убрать принудительную сортировку при активном фильтре; категорийный порядок нужен только когда filter пуст (что уже обрабатывается отдельной веткой).

**Files:**
- Modify: `ui/tui/view/palette_modal.go`

- [ ] **Step 1: Убрать `sort.Ints(indices)` из `applyFilter`**

Заменить тело `applyFilter` (строки ~114-136):

```go
func (m *PaletteModal) applyFilter() {
	m.cursor = 0
	if m.filter == "" {
		m.filtered = m.all
		return
	}
	names := make([]string, len(m.all))
	for i, c := range m.all {
		names[i] = c.Name
	}
	matches := fuzzy.Find(m.filter, names)
	// Preserve fuzzy relevance order — best match first.
	// Category grouping only applies when filter is empty (see branch above).
	m.filtered = make([]ModalCommand, 0, len(matches))
	for _, mt := range matches {
		m.filtered = append(m.filtered, m.all[mt.Index])
	}
}
```

Также удалить неиспользуемый импорт `"sort"` из шапки файла.

- [ ] **Step 2: Запустить тесты**

```
go test ./ui/tui/view/... -v
go build ./...
```

Ожидаемый вывод: `PASS` / `build OK`.

- [ ] **Step 3: Коммит**

```
git add ui/tui/view/palette_modal.go
git commit -m "fix(palette): restore fuzzy relevance order in PaletteModal — sort by score, not index"
```

---

## Итоговая проверка

- [ ] Запустить полный набор тестов:

```
go vet ./...
go test ./...
```

Ожидаемый вывод: `go vet` без предупреждений, все тесты `PASS`.
