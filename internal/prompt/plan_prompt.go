package prompt

import (
	"fmt"
	"strings"
)

// BuildPlanPrompt builds a prompt specifically for generating a plan
func BuildPlanPrompt(files []FileSnippet, userQuery string) string {
	var promptBuilder strings.Builder

	promptBuilder.WriteString("Ты ассистент по коду.\n")
	promptBuilder.WriteString("Проанализируй задачу и создай ПЛАН изменений.\n\n")

	if len(files) > 0 {
		promptBuilder.WriteString("Доступные файлы проекта:\n")
		for _, file := range files {
			promptBuilder.WriteString(fmt.Sprintf("- %s\n", file.Path))
		}
		promptBuilder.WriteString("\n")
	}

	promptBuilder.WriteString("Задача пользователя:\n")
	promptBuilder.WriteString(userQuery)
	promptBuilder.WriteString("\n\n")

	promptBuilder.WriteString("ВАЖНО: Верни ТОЛЬКО JSON в формате:\n")
	promptBuilder.WriteString("{\n")
	promptBuilder.WriteString("  \"steps\": [\n")
	promptBuilder.WriteString("    {\n")
	promptBuilder.WriteString("      \"file_path\": \"path/to/file.go\",\n")
	promptBuilder.WriteString("      \"action\": \"modify\",\n")
	promptBuilder.WriteString("      \"summary\": \"краткое описание изменения\"\n")
	promptBuilder.WriteString("    }\n")
	promptBuilder.WriteString("  ]\n")
	promptBuilder.WriteString("}\n\n")

	promptBuilder.WriteString("Действия:\n")
	promptBuilder.WriteString("- \"modify\" - изменить существующий файл\n")
	promptBuilder.WriteString("- \"create\" - создать новый файл\n")
	promptBuilder.WriteString("- \"delete\" - удалить файл (пока не поддерживается)\n\n")

	promptBuilder.WriteString("Верни ТОЛЬКО JSON, без дополнительного текста.\n")

	return promptBuilder.String()
}
