package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/search"
)

var (
	sinkPrompt string
	sinkFiles  int
	sinkBytes  int
)

const (
	benchQuery   = "add logging to main.go"
	benchLimitKB = 50
)

var benchExcludeDirs = []string{".git", "node_modules", "dist", "build", ".orchestra"}

// fileSnippet is a file path + content pair used to build benchmark prompts.
type fileSnippet struct {
	Path    string
	Content string
}

func BenchmarkContext_Direct_Small(b *testing.B) {
	benchmarkDirect(b, projectRootSmall())
}

func BenchmarkContext_Direct_Medium(b *testing.B) {
	root, err := projectRootMedium()
	if err != nil {
		b.Fatalf("medium project setup failed: %v", err)
	}
	benchmarkDirect(b, root)
}

func BenchmarkContext_Direct_Large(b *testing.B) {
	if !benchLargeEnabled() {
		b.Skip("set ORCHESTRA_BENCH_LARGE=1 to enable")
	}
	root, err := projectRootLarge()
	if err != nil {
		b.Fatalf("large project setup failed: %v", err)
	}
	benchmarkDirect(b, root)
}

func benchmarkDirect(b *testing.B, projectRoot string) {
	b.Helper()
	b.ReportAllocs()

	res, err := buildDirect(projectRoot)
	if err != nil {
		b.Fatalf("direct warmup failed: %v", err)
	}
	sinkPrompt = res

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := buildDirect(projectRoot)
		if err != nil {
			b.Fatalf("direct failed: %v", err)
		}
		sinkPrompt = res
	}
}

func buildDirect(projectRoot string) (string, error) {
	var focusFiles []string
	searchOpts := search.DefaultOptions()
	searchOpts.MaxMatchesPerFile = 2

	matches, err := search.SearchInProject(projectRoot, benchQuery, benchExcludeDirs, searchOpts)
	if err == nil && len(matches) > 0 {
		focusSet := make(map[string]struct{}, len(matches))
		for _, m := range matches {
			if _, ok := focusSet[m.FilePath]; ok {
				continue
			}
			focusSet[m.FilePath] = struct{}{}
			rel, relErr := filepath.Rel(projectRoot, m.FilePath)
			if relErr == nil {
				focusFiles = append(focusFiles, rel)
			} else {
				focusFiles = append(focusFiles, m.FilePath)
			}
		}
	}

	files, err := collectFiles(projectRoot, focusFiles, benchLimitKB*1024)
	if err != nil {
		return "", err
	}
	sinkFiles = len(files)
	bytesTotal, _ := statsFromSnippets(files)
	sinkBytes = bytesTotal
	return buildCodePrompt(files, benchQuery), nil
}

func statsFromSnippets(files []fileSnippet) (bytesTotal int, truncatedFiles int) {
	for _, f := range files {
		bytesTotal += len(f.Content)
	}
	return bytesTotal, 0
}

// collectFiles walks the project, prioritising focusFiles, up to limitBytes total.
func collectFiles(projectRoot string, focusFiles []string, limitBytes int) ([]fileSnippet, error) {
	projectRootAbs, _ := filepath.Abs(projectRoot)
	focusSet := make(map[string]bool, len(focusFiles))
	for _, f := range focusFiles {
		var abs string
		if filepath.IsAbs(f) {
			abs = f
		} else {
			abs = filepath.Join(projectRootAbs, f)
		}
		abs, _ = filepath.Abs(abs)
		focusSet[abs] = true
	}

	excludeMap := make(map[string]bool, len(benchExcludeDirs))
	for _, d := range benchExcludeDirs {
		excludeMap[d] = true
	}

	var focus, others []fileSnippet
	err := filepath.Walk(projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			relPath, _ := filepath.Rel(projectRoot, path)
			if excludeMap[filepath.Base(path)] || excludeMap[relPath] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".orchestra.bak") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(projectRoot, path)
		absPath, _ := filepath.Abs(path)
		s := fileSnippet{Path: relPath, Content: string(data)}
		if focusSet[absPath] {
			focus = append(focus, s)
		} else {
			others = append(others, s)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk failed: %w", err)
	}

	sort.Slice(focus, func(i, j int) bool { return focus[i].Path < focus[j].Path })
	sort.Slice(others, func(i, j int) bool { return others[i].Path < others[j].Path })

	var out []fileSnippet
	total := 0
	for _, group := range [][]fileSnippet{focus, others} {
		for _, f := range group {
			if total >= limitBytes {
				break
			}
			if total+len(f.Content) > limitBytes {
				continue
			}
			out = append(out, f)
			total += len(f.Content)
		}
	}
	return out, nil
}

func buildCodePrompt(files []fileSnippet, userQuery string) string {
	var b strings.Builder
	b.WriteString("You are a coding assistant.\nHere are project files:\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "FILE: %s\n<<<CODE\n%s\n>>>CODE\n\n", f.Path, f.Content)
	}
	b.WriteString("User task:\n")
	b.WriteString(userQuery)
	b.WriteString("\n")
	return b.String()
}
