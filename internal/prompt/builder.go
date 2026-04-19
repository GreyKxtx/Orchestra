package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileSnippet represents a file with its content
type FileSnippet struct {
	Path    string
	Content string
}

// BuildParams contains parameters for building context
type BuildParams struct {
	ProjectRoot string
	LimitKB     int // Context limit in kilobytes (will be converted to bytes: LimitKB * 1024)
	ExcludeDirs []string
	FocusFiles  []string // Files to prioritize in context (from search results)
}

// BuildResult contains the built context
type BuildResult struct {
	Files  []FileSnippet
	Prompt string
}

// BuildContext builds context from project files
func BuildContext(params BuildParams, userQuery string) (*BuildResult, error) {
	var files []FileSnippet
	totalSize := 0

	excludeMap := make(map[string]bool)
	for _, dir := range params.ExcludeDirs {
		excludeMap[dir] = true
	}

	// Build set of focus files for quick lookup (normalize paths)
	focusSet := make(map[string]bool)
	projectRootAbs, _ := filepath.Abs(params.ProjectRoot)
	for _, focusFile := range params.FocusFiles {
		// Focus files can be relative or absolute
		var absPath string
		if filepath.IsAbs(focusFile) {
			absPath = focusFile
		} else {
			absPath = filepath.Join(projectRootAbs, focusFile)
		}
		absPath, _ = filepath.Abs(absPath)
		focusSet[absPath] = true
	}

	// First pass: collect all files, separating focus and others
	var focusFiles []FileSnippet
	var otherFiles []FileSnippet

	// Walk project directory
	err := filepath.Walk(params.ProjectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't read
		}

		// Skip directories
		if info.IsDir() {
			// Check if directory should be excluded
			relPath, _ := filepath.Rel(params.ProjectRoot, path)
			dirName := filepath.Base(path)
			if excludeMap[dirName] || excludeMap[relPath] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip backup files
		if strings.HasSuffix(path, ".orchestra.bak") {
			return nil
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		relPath, _ := filepath.Rel(params.ProjectRoot, path)
		absPath, _ := filepath.Abs(path)

		snippet := FileSnippet{
			Path:    relPath,
			Content: string(data),
		}

		// Prioritize focus files
		if focusSet[absPath] {
			focusFiles = append(focusFiles, snippet)
		} else {
			otherFiles = append(otherFiles, snippet)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk project: %w", err)
	}

	// Sort each group by path for consistency
	sort.Slice(focusFiles, func(i, j int) bool {
		return focusFiles[i].Path < focusFiles[j].Path
	})
	sort.Slice(otherFiles, func(i, j int) bool {
		return otherFiles[i].Path < otherFiles[j].Path
	})

	limitBytes := params.LimitKB * 1024

	// Add focus files first (up to limit)
	for _, f := range focusFiles {
		if totalSize >= limitBytes {
			break
		}
		fileSize := len(f.Content)
		if totalSize+fileSize > limitBytes {
			// Skip if doesn't fit (could log warning in future)
			continue
		}
		files = append(files, f)
		totalSize += fileSize
	}

	// Then add other files until limit
	for _, f := range otherFiles {
		if totalSize >= limitBytes {
			break
		}
		fileSize := len(f.Content)
		if totalSize+fileSize > limitBytes {
			continue
		}
		files = append(files, f)
		totalSize += fileSize
	}

	return &BuildResult{
		Files:  files,
		Prompt: BuildCodePrompt(files, userQuery),
	}, nil
}

// BuildCodePrompt builds the prompt for code generation from a prepared file list.
// It is used by both direct mode (BuildContext) and daemon mode (/context -> CLI apply).
func BuildCodePrompt(files []FileSnippet, userQuery string) string {
	var promptBuilder strings.Builder
	promptBuilder.WriteString("Ты ассистент по коду.\n")
	promptBuilder.WriteString("Вот список файлов и их содержимое.\n\n")

	for _, file := range files {
		promptBuilder.WriteString(fmt.Sprintf("FILE: %s\n", file.Path))
		promptBuilder.WriteString("<<<CODE\n")
		promptBuilder.WriteString(file.Content)
		promptBuilder.WriteString("\n>>>CODE\n\n")
	}

	promptBuilder.WriteString("Задача пользователя:\n")
	promptBuilder.WriteString(userQuery)
	promptBuilder.WriteString("\n\n")
	promptBuilder.WriteString("ВАЖНО: Сгенерируй изменения в формате:\n")
	promptBuilder.WriteString("---FILE: path/to/file.go\n")
	promptBuilder.WriteString("<<<BLOCK\n")
	promptBuilder.WriteString("старый код\n")
	promptBuilder.WriteString(">>>BLOCK\n")
	promptBuilder.WriteString("новый код\n")
	promptBuilder.WriteString("---END\n\n")
	promptBuilder.WriteString("ПРОТОКОЛ:\n")
	promptBuilder.WriteString("1. Если ЗАМЕНЯЕШЬ существующий код:\n")
	promptBuilder.WriteString("   - Укажи ТОЧНЫЙ старый код в <<<BLOCK\n")
	promptBuilder.WriteString("   - Укажи новый код в >>>BLOCK\n")
	promptBuilder.WriteString("   - Старый код должен ТОЧНО совпадать с содержимым файла\n\n")
	promptBuilder.WriteString("2. Если ДОБАВЛЯЕШЬ новый код (функцию, метод, блок):\n")
	promptBuilder.WriteString("   - ВСЕГДА указывай <<<BLOCK, но оставь его ПУСТЫМ\n")
	promptBuilder.WriteString("   - Укажи только новый код после >>>BLOCK\n")
	promptBuilder.WriteString("   - Новый код будет добавлен в КОНЕЦ файла\n\n")
	promptBuilder.WriteString("3. Если заменяешь ВЕСЬ файл:\n")
	promptBuilder.WriteString("   - Укажи весь старый файл в <<<BLOCK\n")
	promptBuilder.WriteString("   - Укажи весь новый файл в >>>BLOCK\n")

	return promptBuilder.String()
}
