# TUI Phase 6 — Visual Redesign + Onboarding Design Spec

## Goal

Полный визуальный рефакторинг TUI по образцу OpenCode: тема, бордеры сообщений, markdown, streaming feedback, статусбар, welcome screen, command palette modal, onboarding flow с выбором провайдера/модели и настройками.

## Color Scheme (Orchestra Theme)

| Роль | Hex | Применение |
|---|---|---|
| Background | `#0d0d0d` | фон всего TUI |
| BackgroundSecondary | `#1a1a1a` | модалки, панели |
| Primary | `#9d7cd8` | фиолетовый — бордер ассистента, выделение |
| Secondary | `#e0af68` | жёлтый — бордер пользователя, акценты |
| Text | `#c0caf5` | основной текст |
| TextMuted | `#565f89` | подсказки, метаданные, бордер модалок |
| Error | `#f7768e` | ошибки |
| Success | `#9ece6a` | статус OK |

---

## Architecture

### Новые файлы

| Файл | Ответственность |
|---|---|
| `ui/tui/theme/theme.go` | интерфейс Theme (20 методов) + `CurrentTheme()` глобал |
| `ui/tui/theme/orchestra.go` | дефолтная тема Orchestra (dark/purple/yellow) |
| `ui/tui/view/statusbar.go` | статусбар (спиннер, модель, % ctx, хинты) |
| `ui/tui/view/palette_modal.go` | command palette modal (Ctrl+K) |
| `ui/tui/view/onboarding.go` | onboarding flow (3 шага) |
| `internal/lmstudio/client.go` | HTTP клиент для LM Studio API |

### Изменяемые файлы

| Файл | Что меняется |
|---|---|
| `ui/tui/view/chat.go` | бордеры сообщений, glamour markdown, welcome screen, streaming cursor |
| `ui/tui/view/header.go` | упрощение до одной строки с ASCII логотипом |
| `ui/tui/view/footer.go` | удаляется — заменяется statusbar |
| `ui/tui/app.go` | новые состояния: `showCommandModal`, `showOnboarding`; Ctrl+K/O биндинги; проверка модели при старте |
| `internal/cli/tui.go` | передача флага onboarding если нет модели |

---

## Section 1: Theme System

`ui/tui/theme/theme.go` — интерфейс:

```go
type Theme interface {
    Background() lipgloss.Color
    BackgroundSecondary() lipgloss.Color
    Primary() lipgloss.Color
    Secondary() lipgloss.Color
    Text() lipgloss.Color
    TextMuted() lipgloss.Color
    Error() lipgloss.Color
    Success() lipgloss.Color
    Warning() lipgloss.Color
    BorderNormal() lipgloss.Color
    BorderFocused() lipgloss.Color
}

var current Theme = &OrchestraTheme{}

func CurrentTheme() Theme { return current }
```

`ui/tui/theme/orchestra.go` — реализация со всеми захардкоженными цветами.

Все view-компоненты читают тему через `theme.CurrentTheme()` — без DI.

---

## Section 2: Layout

```
┌────────────────────────────────────────────────────────┐
│  ♪ Orchestra · qwen3.6-27b · my-project        v0.x   │  header (1 строка)
├────────────────────────────────────────────────────────┤
│                                                        │
│  ▏ Сообщение пользователя                              │  жёлтый ThickBorder
│                                                        │
│  ▏ Ответ ассистента с markdown                         │  фиолетовый ThickBorder
│  ▏ ▸ fs.read → main.go                                 │
│  ▏ qwen3.6-27b · 3.2s                                  │
│                                                        │
├────────────────────────────────────────────────────────┤
│  > _                                                   │  textarea
├────────────────────────────────────────────────────────┤
│  ⠋ Thinking…  ·  qwen3.6-27b  ·  ctx 14%  ·  ctrl+k   │  statusbar
└────────────────────────────────────────────────────────┘
```

Footer (`view/footer.go`) удаляется. Высота layout: header(1) + chat(h-5) + input(3) + statusbar(1).

---

## Section 3: Messages (chat.go)

### Рендер сообщения

```go
func renderMessage(text string, isUser bool, width int, meta string) string {
    t := theme.CurrentTheme()
    borderColor := t.Primary()      // assistant = фиолетовый
    if isUser {
        borderColor = t.Secondary() // user = жёлтый
    }
    content := renderMarkdown(text, width-4)
    if meta != "" {
        content = lipgloss.JoinVertical(lipgloss.Left, content,
            lipgloss.NewStyle().Foreground(t.TextMuted()).Render(meta))
    }
    return lipgloss.NewStyle().
        BorderLeft(true).
        BorderStyle(lipgloss.ThickBorder()).
        BorderForeground(borderColor).
        PaddingLeft(1).
        Width(width - 2).
        Render(content)
}
```

### Streaming cursor

Пока `agentBusy == true` — в конце последнего токена добавляется `▋` (мигает через `tea.Tick(500ms)`). По завершении убирается, появляется `meta = "modelName · Xs"`.

### Markdown

```go
func renderMarkdown(text string, width int) string {
    r, _ := glamour.NewTermRenderer(
        glamour.WithStylePath("dark"),
        glamour.WithWordWrap(width),
    )
    out, err := r.Render(text)
    if err != nil { return text }
    return strings.TrimSpace(out)
}
```

Glamour рендерится с нашей тёмной темой. Ширина = viewport width - 4 (отступ бордера + паддинг).

### Welcome Screen

Показывается когда `len(messages) == 0`. Центрируется через `lipgloss.Place`:

```
         ██████╗ ██████╗  ██████╗
        ██╔═══██╗██╔══██╗██╔════╝
        ██║   ██║██████╔╝██║
        ██║   ██║██╔══██╗██║
        ╚██████╔╝██║  ██║╚██████╗
         ╚═════╝ ╚═╝  ╚═╝ ╚═════╝

                                        v0.x
              AI coding assistant
              ────────────────────────────
              📁  orchestra-sandbox
              🤖  qwen3.6-27b  (LM Studio)
              ✓   core connected

              Напиши сообщение чтобы начать…
```

---

## Section 4: Status Bar (statusbar.go)

```go
type StatusBar struct {
    width      int
    agentBusy  bool
    spinFrame  int           // 0-9, обновляется через tea.Tick
    model      string
    ctxPercent int           // 0-100
    errorMsg   string
}
```

Render:
- Левая часть: `⠋ Thinking…` (busy) / `●  Ready` (idle) / `✗  <error>` (error)
- Правая часть: `qwen3.6-27b · ctx 14% · ctrl+k команды · ctrl+o модель`
- При ctx > 80% — жёлтый цвет; при > 95% — красный
- Полная ширина терминала, фон `BackgroundSecondary`

`ctxPercent` вычисляется из `usage.InputTokens / num_ctx * 100` при каждом `EventStepDone`.

---

## Section 5: Command Palette Modal (palette_modal.go)

Открывается Ctrl+K, накладывается поверх чата через `lipgloss.Place` (центр экрана).

```go
type PaletteModal struct {
    commands []SlashCmd   // те же 8 команд что в SlashPalette
    filter   string
    cursor   int
    width    int
    height   int
    active   bool
}
```

Render: `lipgloss.RoundedBorder()` + `BorderForeground(TextMuted)`. Выбранный элемент: `Background(Primary).Foreground(Background).Bold(true)`. Поле поиска сверху, список снизу.

Биндинги: `↑/↓` навигация, `Enter` выполнить, `Esc` закрыть, любой символ — фильтрация.

Slash inline режим (`/` в инпуте) **остаётся без изменений** — параллельно с модалкой.

---

## Section 6: Onboarding Flow (onboarding.go)

Показывается при старте если `cfg.LLM.Model == ""`. Три шага в одном компоненте `OnboardingView`.

### Шаг 1: Выбор провайдера

```go
type Provider struct {
    Name     string
    Endpoint string
}

var SupportedProviders = []Provider{
    {"LM Studio", "http://localhost:1234"},
    {"Ollama",    "http://localhost:11434"},
    {"OpenAI",    "https://api.openai.com"},
    {"Custom",    ""},
}
```

UI: список с выделением (Primary background). Custom → поле ввода URL.

### Шаг 2: Выбор модели

После выбора провайдера — HTTP запрос к LM Studio (через `lmstudio.Client`). Список моделей с `ctx: N` и `✓ loaded` маркером для активной модели.

```go
type RemoteModel struct {
    ID               string
    MaxContextLength int64
    IsLoaded         bool
}
```

### Шаг 3: Настройки модели

```
temperature    [ 0.20 ]   // float, 0.0–2.0, шаг 0.05
max_tokens     [ 8192 ]   // int
num_ctx        [20480 ]   // из API, редактируемо
thinking mode  [  off ]   // bool → enable_thinking: true/false в extra_body
```

Навигация: `Tab`/`↑↓` между полями, `←→` изменение значения.

### Сохранение

При Enter на шаге 3 — `config.Save()` пишет `.orchestra.yml` с новыми значениями (создаёт файл если не существует, обновляет если существует — через `os.WriteFile` с полным YAML):
- `llm.model`, `llm.api_base`, `llm.temperature`, `llm.max_tokens`
- `llm.extra_body.num_ctx`, `llm.extra_body.chat_template_kwargs.enable_thinking`

После сохранения — TUI перезапускает RPC клиент и переходит к welcome screen.

Ctrl+O из чата открывает шаг 2 напрямую (пропуская выбор провайдера — используется текущий).

---

## Section 7: LM Studio Client (internal/lmstudio/client.go)

```go
type Client struct {
    Endpoint string // e.g. "http://localhost:1234"
}

func (c *Client) ListModels() ([]RemoteModel, error) {
    // 1. Пробуем GET {endpoint}/api/v0/models (LM Studio beta)
    //    фильтр: object=="model" && type=="llm"
    // 2. Fallback: GET {endpoint}/v1/models (OpenAI-compatible)
    // Возвращаем []RemoteModel{ID, MaxContextLength, IsLoaded}
}
```

Timeout: 5 секунд. Ошибка соединения → показывается в onboarding как `✗ LM Studio недоступен — убедись что он запущен`.

---

## Ключевые инварианты

- Без выбранной модели (`llm.model == ""`) — инпут заблокирован, показывается onboarding
- `theme.CurrentTheme()` — единственный источник цветов, без DI
- Glamour рендер происходит при каждом `SetMessages()` — не кешируется (достаточно быстро для TUI)
- `view/footer.go` удаляется полностью, все хинты в statusbar
- Onboarding не требует запущенного `orchestra core` — работает до RPC подключения
