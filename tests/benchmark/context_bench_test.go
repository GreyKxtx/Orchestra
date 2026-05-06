package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/daemon"
	"github.com/orchestra/orchestra/internal/search"
)

var (
	sinkPrompt string
	sinkFiles  int
	sinkBytes  int
	sinkTrunc  int
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

func BenchmarkContext_DaemonInProc_Small(b *testing.B) {
	benchmarkDaemonInProc(b, projectRootSmall())
}

func BenchmarkContext_DaemonInProc_Medium(b *testing.B) {
	root, err := projectRootMedium()
	if err != nil {
		b.Fatalf("medium project setup failed: %v", err)
	}
	benchmarkDaemonInProc(b, root)
}

func BenchmarkContext_DaemonInProc_Large(b *testing.B) {
	if !benchLargeEnabled() {
		b.Skip("set ORCHESTRA_BENCH_LARGE=1 to enable")
	}
	root, err := projectRootLarge()
	if err != nil {
		b.Fatalf("large project setup failed: %v", err)
	}
	benchmarkDaemonInProc(b, root)
}

func BenchmarkContext_DaemonHTTP_Small(b *testing.B) {
	benchmarkDaemonHTTP(b, projectRootSmall())
}

func BenchmarkContext_DaemonHTTP_Medium(b *testing.B) {
	root, err := projectRootMedium()
	if err != nil {
		b.Fatalf("medium project setup failed: %v", err)
	}
	benchmarkDaemonHTTP(b, root)
}

func BenchmarkContext_DaemonHTTP_Large(b *testing.B) {
	if !benchLargeEnabled() {
		b.Skip("set ORCHESTRA_BENCH_LARGE=1 to enable")
	}
	root, err := projectRootLarge()
	if err != nil {
		b.Fatalf("large project setup failed: %v", err)
	}
	benchmarkDaemonHTTP(b, root)
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

func benchmarkDaemonInProc(b *testing.B, projectRoot string) {
	b.Helper()
	b.ReportAllocs()

	srv := newBenchServer(b, projectRoot)
	if _, err := srv.Refresh(context.Background()); err != nil {
		b.Fatalf("daemon refresh failed: %v", err)
	}

	promptStr := buildViaDaemonInProc(b, srv)
	sinkPrompt = promptStr

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		promptStr := buildViaDaemonInProc(b, srv)
		sinkPrompt = promptStr
	}
}

func buildViaDaemonInProc(b *testing.B, srv *daemon.Server) string {
	b.Helper()

	resp, err := srv.Context(context.Background(), daemon.ContextRequest{
		Query:       benchQuery,
		LimitKB:     benchLimitKB,
		ExcludeDirs: benchExcludeDirs,
		Limits: &daemon.ContextLimits{
			MaxFiles:        30,
			MaxTotalBytes:   int64(benchLimitKB) * 1024,
			MaxBytesPerFile: daemon.DefaultMaxBytesPerFile,
		},
	})
	if err != nil {
		b.Fatalf("daemon inproc context failed: %v", err)
	}

	files := make([]fileSnippet, 0, len(resp.Files))
	bytesTotal := 0
	trunc := 0
	for _, f := range resp.Files {
		files = append(files, fileSnippet{Path: f.Path, Content: f.Content})
		bytesTotal += len(f.Content)
		if f.Truncated {
			trunc++
		}
	}
	sinkFiles = len(files)
	sinkBytes = bytesTotal
	sinkTrunc = trunc
	return buildCodePrompt(files, benchQuery)
}

func benchmarkDaemonHTTP(b *testing.B, projectRoot string) {
	b.Helper()
	b.ReportAllocs()

	baseURL, token, stop := startBenchDaemonHTTP(b, projectRoot)
	defer stop()

	client := daemon.NewClientWithToken(baseURL, token)

	p := buildViaDaemonHTTP(b, client)
	sinkPrompt = p

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := buildViaDaemonHTTP(b, client)
		sinkPrompt = p
	}
}

func startBenchDaemonHTTP(b *testing.B, projectRoot string) (baseURL string, token string, stop func()) {
	b.Helper()

	srv := newBenchServer(b, projectRoot)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		info, ok, err := daemon.ReadDiscovery(projectRoot)
		if err == nil && ok && info != nil && info.URL != "" {
			baseURL = info.URL
			token = info.Token
			break
		}
		if time.Now().After(deadline) {
			cancel()
			_ = <-errCh
			b.Fatalf("daemon did not start in time (discovery not available)")
		}
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}

	stop = func() {
		cancel()
		_ = <-errCh
	}
	return baseURL, token, stop
}

func buildViaDaemonHTTP(b *testing.B, client *daemon.Client) string {
	b.Helper()

	if _, err := client.Health(context.Background()); err != nil {
		b.Fatalf("daemon health failed: %v", err)
	}
	resp, err := client.Context(context.Background(), daemon.ContextRequest{
		Query:       benchQuery,
		LimitKB:     benchLimitKB,
		ExcludeDirs: benchExcludeDirs,
		Limits: &daemon.ContextLimits{
			MaxFiles:        30,
			MaxTotalBytes:   int64(benchLimitKB) * 1024,
			MaxBytesPerFile: daemon.DefaultMaxBytesPerFile,
		},
	})
	if err != nil {
		b.Fatalf("daemon http context failed: %v", err)
	}

	files := make([]fileSnippet, 0, len(resp.Files))
	bytesTotal := 0
	trunc := 0
	for _, f := range resp.Files {
		files = append(files, fileSnippet{Path: f.Path, Content: f.Content})
		bytesTotal += len(f.Content)
		if f.Truncated {
			trunc++
		}
	}
	sinkFiles = len(files)
	sinkBytes = bytesTotal
	sinkTrunc = trunc
	return buildCodePrompt(files, benchQuery)
}

func newBenchServer(b *testing.B, projectRoot string) *daemon.Server {
	b.Helper()

	srv, err := daemon.NewServer(daemon.ServerConfig{
		ProjectRoot:       projectRoot,
		Address:           "127.0.0.1",
		Port:              0,
		ExcludeDirs:       benchExcludeDirs,
		SkipBackups:       true,
		ScanInterval:      24 * time.Hour,
		MaxCacheFileBytes: daemon.DefaultMaxCacheFileBytes,
		CacheEnabled:      true,
		CachePath:         filepath.Join(projectRoot, ".orchestra", "cache.json"),
	})
	if err != nil {
		b.Fatalf("NewServer failed: %v", err)
	}
	return srv
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

// buildCodePrompt formats files into the legacy v0.2 <<<BLOCK prompt style used by daemon benchmarks.
func buildCodePrompt(files []fileSnippet, userQuery string) string {
	var b strings.Builder
	b.WriteString("Ты ассистент по коду.\nВот список файлов и их содержимое.\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "FILE: %s\n<<<CODE\n%s\n>>>CODE\n\n", f.Path, f.Content)
	}
	b.WriteString("Задача пользователя:\n")
	b.WriteString(userQuery)
	b.WriteString("\n")
	return b.String()
}
