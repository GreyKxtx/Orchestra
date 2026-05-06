package prompt

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultMemoryCap = 8 * 1024 // 8 KB

// LoadProjectMemory reads project memory from the workspace and returns it
// formatted for injection into the system prompt.
//
// All three sources are read and concatenated (additive):
//  1. <workspace_root>/ORCHESTRA.md
//  2. <workspace_root>/.orchestra/memory/*.md  (sorted, concatenated)
//  3. ~/.orchestra/memory.md
//
// The combined content is capped at maxBytes. Returns empty string if nothing found.
func LoadProjectMemory(workspaceRoot string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultMemoryCap
	}

	content := readAllSources(workspaceRoot)
	if content == "" {
		return ""
	}

	content = strings.TrimSpace(content)
	if len(content) > maxBytes {
		content = content[:maxBytes] + "\n...(truncated)"
	}

	return "<project_memory>\n" + content + "\n</project_memory>"
}

func readAllSources(workspaceRoot string) string {
	var parts []string

	// 1. <workspace_root>/ORCHESTRA.md
	if workspaceRoot != "" {
		if data, err := os.ReadFile(filepath.Join(workspaceRoot, "ORCHESTRA.md")); err == nil {
			if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	}

	// 2. <workspace_root>/.orchestra/memory/*.md (sorted, concatenated)
	if workspaceRoot != "" {
		memDir := filepath.Join(workspaceRoot, ".orchestra", "memory")
		if content := readMemoryDir(memDir); content != "" {
			parts = append(parts, content)
		}
	}

	// 3. ~/.orchestra/memory.md
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".orchestra", "memory.md")); err == nil {
			if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}

func readMemoryDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)

	var parts []string
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}
