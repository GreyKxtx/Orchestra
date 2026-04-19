package benchmark

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/daemon"
	"github.com/orchestra/orchestra/internal/prompt"
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

	// Warmup once (exclude from timing).
	res, err := buildDirect(projectRoot)
	if err != nil {
		b.Fatalf("direct warmup failed: %v", err)
	}
	sinkPrompt = res.Prompt

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := buildDirect(projectRoot)
		if err != nil {
			b.Fatalf("direct failed: %v", err)
		}
		sinkPrompt = res.Prompt
		sinkFiles = len(res.Files)
		bytesTotal, trunc := statsFromSnippets(res.Files)
		sinkBytes = bytesTotal
		sinkTrunc = trunc
	}
}

func buildDirect(projectRoot string) (*prompt.BuildResult, error) {
	// Mimic CLI direct mode: search -> focus files -> BuildContext.
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

	return prompt.BuildContext(prompt.BuildParams{
		ProjectRoot: projectRoot,
		LimitKB:     benchLimitKB,
		ExcludeDirs: benchExcludeDirs,
		FocusFiles:  focusFiles,
	}, benchQuery)
}

func benchmarkDaemonInProc(b *testing.B, projectRoot string) {
	b.Helper()
	b.ReportAllocs()

	srv := newBenchServer(b, projectRoot)
	// Warm scan/index before timing.
	if _, err := srv.Refresh(context.Background()); err != nil {
		b.Fatalf("daemon refresh failed: %v", err)
	}

	// Warmup once.
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

	files := make([]prompt.FileSnippet, 0, len(resp.Files))
	bytesTotal := 0
	trunc := 0
	for _, f := range resp.Files {
		files = append(files, prompt.FileSnippet{Path: f.Path, Content: f.Content})
		bytesTotal += len(f.Content)
		if f.Truncated {
			trunc++
		}
	}
	sinkFiles = len(files)
	sinkBytes = bytesTotal
	sinkTrunc = trunc
	return prompt.BuildCodePrompt(files, benchQuery)
}

func benchmarkDaemonHTTP(b *testing.B, projectRoot string) {
	b.Helper()
	b.ReportAllocs()

	baseURL, token, stop := startBenchDaemonHTTP(b, projectRoot)
	defer stop()

	client := daemon.NewClientWithToken(baseURL, token)

	// Warmup once (exclude from timing).
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

	// Wait for discovery file (avoids data races on srv.URL()).
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

	// Mimic CLI-ish behavior: health + context.
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

	files := make([]prompt.FileSnippet, 0, len(resp.Files))
	bytesTotal := 0
	trunc := 0
	for _, f := range resp.Files {
		files = append(files, prompt.FileSnippet{Path: f.Path, Content: f.Content})
		bytesTotal += len(f.Content)
		if f.Truncated {
			trunc++
		}
	}
	sinkFiles = len(files)
	sinkBytes = bytesTotal
	sinkTrunc = trunc
	return prompt.BuildCodePrompt(files, benchQuery)
}

func newBenchServer(b *testing.B, projectRoot string) *daemon.Server {
	b.Helper()

	// Use a long scan interval so periodic scan doesn't skew timings.
	srv, err := daemon.NewServer(daemon.ServerConfig{
		ProjectRoot:       projectRoot,
		Address:           "127.0.0.1",
		Port:              0, // ephemeral
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

func statsFromSnippets(files []prompt.FileSnippet) (bytesTotal int, truncatedFiles int) {
	// Direct mode doesn't report truncation per file; treat as 0.
	for _, f := range files {
		bytesTotal += len(f.Content)
	}
	return bytesTotal, 0
}
