package lessons

import "path/filepath"

// languageFromExt maps a file extension to the language-bucket name used to
// build a dept key. Kept local (not shared with internal/ckg.LanguageFromExt)
// to avoid pulling internal/lessons into the ckg/tree-sitter dependency graph
// for one small lookup table.
func languageFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".kt", ".kts":
		return "kotlin"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	default:
		return ""
	}
}

// InferDeptFromFiles picks a dept key ("<language>_engineering") from the
// languages of the given files, majority wins, ties broken by the first file
// in the slice. Returns "" when no file maps to a known language (matching
// NormalizeDept's own empty-string fallback to "engineering").
func InferDeptFromFiles(files []string) string {
	counts := make(map[string]int, len(files))
	order := make([]string, 0, len(files))
	for _, f := range files {
		lang := languageFromExt(filepath.Ext(f))
		if lang == "" {
			continue
		}
		if counts[lang] == 0 {
			order = append(order, lang)
		}
		counts[lang]++
	}
	if len(order) == 0 {
		return ""
	}
	best := order[0]
	for _, lang := range order[1:] {
		if counts[lang] > counts[best] {
			best = lang
		}
	}
	return best + "_engineering"
}
