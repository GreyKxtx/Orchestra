package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// WorkspaceSnapshot is a minimal "IDE snapshot" attached to each user request.
// In CLI mode, most fields are best-effort (env-driven).
type WorkspaceSnapshot struct {
	OS            string
	Shell         string
	WorkspaceRoot string
	IsGitRepo     bool

	ActiveFile string
	CursorLine int
	CursorCol  int
	OpenFiles  []string

	TerminalsPath string
	ChangedFiles  []string
}

// BuildUserInfoSnapshot builds a best-effort snapshot of the current environment.
// It intentionally avoids expensive calls (no git status, no filesystem scan beyond ".git").
func BuildUserInfoSnapshot(workspaceRoot string) WorkspaceSnapshot {
	root := strings.TrimSpace(workspaceRoot)
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}

	// OS + arch is stable and available everywhere.
	osID := runtime.GOOS + "/" + runtime.GOARCH

	// Best-effort shell detection (overrideable for IDE/CI).
	shell := strings.TrimSpace(os.Getenv("ORCH_SHELL"))
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = strings.TrimSpace(os.Getenv("ComSpec"))
		} else {
			shell = strings.TrimSpace(os.Getenv("SHELL"))
		}
	}

	isGit := false
	if root != "" {
		if st, err := os.Stat(filepath.Join(root, ".git")); err == nil && st != nil {
			isGit = true
		}
	}

	activeFile := strings.TrimSpace(os.Getenv("ORCH_ACTIVE_FILE"))
	cursorLine := parseIntEnv("ORCH_CURSOR_LINE")
	cursorCol := parseIntEnv("ORCH_CURSOR_COL")

	openFiles := splitListEnv("ORCH_OPEN_FILES")
	terminalsPath := strings.TrimSpace(os.Getenv("ORCH_TERMINALS_PATH"))
	changedFiles := splitListEnv("ORCH_CHANGED_FILES")

	return WorkspaceSnapshot{
		OS:            osID,
		Shell:         shell,
		WorkspaceRoot: root,
		IsGitRepo:     isGit,
		ActiveFile:    activeFile,
		CursorLine:    cursorLine,
		CursorCol:     cursorCol,
		OpenFiles:     openFiles,
		TerminalsPath: terminalsPath,
		ChangedFiles:  changedFiles,
	}
}

func parseIntEnv(key string) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func splitListEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	// Support both ";" (Windows) and "\n" separators.
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// DetectPromptFamily infers model family from the model name string.
// Returns one of: "openai", "qwen", "llama", "mistral", "deepseek", "gemma".
// Falls back to "openai" if unrecognized.
func DetectPromptFamily(modelName string) string {
	lower := strings.ToLower(modelName)
	switch {
	case strings.Contains(lower, "qwen"):
		return "qwen"
	case strings.Contains(lower, "llama"):
		return "llama"
	case strings.Contains(lower, "mistral") || strings.Contains(lower, "mixtral"):
		return "mistral"
	case strings.Contains(lower, "deepseek"):
		return "deepseek"
	case strings.Contains(lower, "gemma"):
		return "gemma"
	default:
		return "openai"
	}
}

// BuildSystemPrompt returns a compact system instruction for the vNext agent.
// Family selects model-family-specific phrasing; empty string uses the default.
func BuildSystemPrompt() string {
	return BuildSystemPromptForFamily("")
}

// BuildSystemPromptForFamily returns a system prompt tuned for the given model family.
// Supported families: "openai" (default), "qwen", "llama", "mistral", "deepseek", "gemma".
func BuildSystemPromptForFamily(family string) string {
	base := strings.TrimSpace(`
Ты — IDE-агент для работы с кодовой базой в workspace. Ты умеешь читать файлы, искать по коду и вносить изменения.

ИНСТРУМЕНТЫ (tool calls):
Используй только реальные tool calls из схемы tools[]. Не имитируй вызовы в тексте.
За один шаг — не более одного tool call.
Доступные инструменты: ls, read, write, edit, glob, grep, symbols и другие из tools[].

ВАЖНО: file.write_atomic, file.search_replace, file.unified_diff — это НЕ tool calls!
Это типы патчей в финальном PatchSet JSON.

ФИНАЛЬНЫЙ ОТВЕТ — два режима:

1. ИНФОРМАЦИОННЫЙ ЗАПРОС (вопрос, анализ, объяснение — без изменений файлов):
   Сначала напиши развёрнутый текстовый ответ пользователю на его вопрос.
   Затем на отдельной строке выведи: {"patches":[]}
   Пример:
     В папке internal находятся пакеты: agent, cli, config, core...
     {"patches":[]}

2. ЗАПРОС НА ИЗМЕНЕНИЕ ФАЙЛОВ (добавить/изменить/создать код):
   Выведи ТОЛЬКО JSON без пояснений:
   {"patches":[{"type":"file.search_replace","path":"...","search":"...","replace":"...","file_hash":"sha256:..."}]}

ТИПЫ ПАТЧЕЙ:
- file.search_replace — точечная правка (нужен file_hash из read)
- file.write_atomic   — новый файл или полная перезапись
- file.unified_diff   — крупный diff (только если search_replace не подходит)

ПРАВИЛА:
- Перед изменением существующего файла всегда делай read и используй точный file_hash из ответа.
- Не делай лишних tool calls — как только собрал нужное, отвечай.
- {"patches":[]} — валидный ответ когда изменений не требуется.
`)

	// For local model families, append a reminder to avoid markdown wrapping.
	switch family {
	case "qwen", "llama", "mistral", "deepseek", "gemma":
		base += "\n\nВАЖНО: Отвечай ТОЛЬКО чистым JSON. Не используй ```json блоки или markdown разметку."
	}
	return base
}

// PlanModeReminder is injected into every user message in plan mode.
const PlanModeReminder = `РЕЖИМ ПЛАНИРОВАНИЯ АКТИВЕН. СТРОГО ЗАПРЕЩЕНО: write и edit (кроме .orchestra/plan.md), bash. Анализируй кодовую базу, задавай вопросы через question, запиши план в .orchestra/plan.md, затем вызови plan_exit.`

// BuildSwitchReminder is injected once when switching from plan to build mode.
const BuildSwitchReminder = `Режим изменён: ПЛАН → BUILD. Теперь разрешены все инструменты. Выполни согласованный план.`

// BuildSystemPromptForMode returns a system prompt tuned for the given agent mode and model family.
// mode: "plan", "explore", or "" / "build" (default).
func BuildSystemPromptForMode(mode, family string) string {
	switch mode {
	case "plan":
		return buildPlanSystemPrompt(family)
	case "explore":
		return buildExploreSystemPrompt()
	default:
		return BuildSystemPromptForFamily(family)
	}
}

func buildPlanSystemPrompt(family string) string {
	base := strings.TrimSpace(`
Ты — агент в режиме ПЛАНИРОВАНИЯ (read-only).

СТРОГО ЗАПРЕЩЕНО: write, edit (кроме .orchestra/plan.md), bash — даже если пользователь просит.
Разрешено: read, ls, glob, grep, symbols, explore, runtime, task_spawn, question, plan_exit.

Твоя задача:
1. Изучи кодовую базу: read / grep / symbols / explore
2. Если доступен <ckg_context> — используй его как стартовую точку для навигации
3. Задавай уточняющие вопросы через question когда нужны трейдоффы
4. Напиши архитектурный план в .orchestra/plan.md через write (единственный разрешённый write)
5. Когда план полностью готов — вызови plan_exit

ФОРМАТ ПЛАНА (.orchestra/plan.md):
## Цель
## Изменяемые файлы (с именами функций)
## Порядок изменений
## Риски и зависимости
`)
	switch family {
	case "qwen", "llama", "mistral", "deepseek", "gemma":
		base += "\n\nВАЖНО: Отвечай ТОЛЬКО чистым JSON для tool calls. Не используй ```json блоки."
	}
	return base
}

func buildExploreSystemPrompt() string {
	return strings.TrimSpace(`
Ты — исследователь кодовой базы (read-only субагент).

Инструменты: read, ls, glob, grep, symbols.
Когда закончил — вызови task_result с кратким структурированным ответом.
Не объясняй что делаешь — только результат.
`)
}

// BuildUserPrompt builds the user-facing message content:
// it includes the IDE snapshot, allowed tool names, and the user's query.
func BuildUserPrompt(userQuery string, snap WorkspaceSnapshot, allowedTools []string) string {
	var b strings.Builder

	b.WriteString("<user_info>\n")
	if snap.OS != "" {
		b.WriteString("os: ")
		b.WriteString(snap.OS)
		b.WriteByte('\n')
	}
	if snap.Shell != "" {
		b.WriteString("shell: ")
		b.WriteString(snap.Shell)
		b.WriteByte('\n')
	}
	if snap.WorkspaceRoot != "" {
		b.WriteString("workspace_root: ")
		b.WriteString(snap.WorkspaceRoot)
		b.WriteByte('\n')
	}
	b.WriteString("is_git_repo: ")
	b.WriteString(fmt.Sprintf("%v", snap.IsGitRepo))
	b.WriteByte('\n')

	if snap.ActiveFile != "" {
		b.WriteString("active_file: ")
		b.WriteString(snap.ActiveFile)
		b.WriteByte('\n')
	}
	if snap.CursorLine != 0 || snap.CursorCol != 0 {
		b.WriteString(fmt.Sprintf("cursor: line=%d col=%d\n", snap.CursorLine, snap.CursorCol))
	}
	if len(snap.OpenFiles) > 0 {
		b.WriteString("open_files:\n")
		for _, f := range snap.OpenFiles {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteByte('\n')
		}
	}
	if snap.TerminalsPath != "" {
		b.WriteString("terminals_path: ")
		b.WriteString(snap.TerminalsPath)
		b.WriteByte('\n')
	}
	if len(snap.ChangedFiles) > 0 {
		b.WriteString("changed_files:\n")
		for _, f := range snap.ChangedFiles {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteByte('\n')
		}
	}
	b.WriteString("</user_info>\n\n")

	// Tools are provided via API "tools" schema, no need to duplicate in text
	// Removed tool list from user prompt to avoid confusion and prompt bloat

	b.WriteString("<user_query>\n")
	b.WriteString(strings.TrimSpace(userQuery))
	b.WriteString("\n</user_query>\n")

	return b.String()
}

// BuildUserPromptWithHistory appends a bounded history tail to the base user prompt.
// maxBytes bounds the final string length (best-effort, in bytes).
func BuildUserPromptWithHistory(baseUserPrompt string, history []string, maxBytes int) string {
	if maxBytes <= 0 {
		return baseUserPrompt + "\n"
	}
	header := strings.TrimRight(baseUserPrompt, "\n") + "\n\nИстория (самые свежие события в конце):\n"
	footer := "\n\nВерни следующий шаг: либо tool call, либо финальный PatchSet JSON.\n"

	budget := maxBytes - len(header) - len(footer)
	if budget <= 0 {
		return strings.TrimSpace(baseUserPrompt)
	}

	var selected []string
	size := 0
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		need := len(item) + 2
		if size+need > budget {
			break
		}
		selected = append(selected, item)
		size += need
	}
	// reverse back to chronological order
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	return header + strings.Join(selected, "\n\n") + footer
}
