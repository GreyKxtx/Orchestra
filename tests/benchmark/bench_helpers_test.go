package benchmark

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type projectSize string

const (
	projectSmall  projectSize = "small"
	projectMedium projectSize = "medium"
	projectLarge  projectSize = "large"
)

type genOpts struct {
	seed        int64
	files       int
	packages    int
	maxDepth    int
	sizeProfile string
}

var (
	onceRepo     sync.Once
	repoRootPath string

	onceMedium sync.Once
	mediumRoot string
	mediumErr  error

	onceLarge sync.Once
	largeRoot string
	largeErr  error
)

func repoRoot() string {
	onceRepo.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			repoRootPath = "."
			return
		}
		repoRootPath = filepath.Clean(filepath.Join(wd, "..", ".."))
	})
	return repoRootPath
}

func projectRootSmall() string {
	// Try real_project/TradingBot first (real project), fallback to test_real_project (old test project)
	candidate := filepath.Join(repoRoot(), "testdata", "small", "real_project", "TradingBot")
	if st, err := os.Stat(candidate); err == nil && st.IsDir() {
		return candidate
	}
	// Fallback to old test project
	return filepath.Join(repoRoot(), "testdata", "small", "test_real_project")
}

func projectRootMedium() (string, error) {
	onceMedium.Do(func() {
		// Try real_project/Lunacy first (real project)
		candidate := filepath.Join(repoRoot(), "testdata", "medium", "real_project", "Lunacy")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			mediumRoot = candidate
			return
		}
		// Try synthetic_project (generated Go project)
		candidate = filepath.Join(repoRoot(), "testdata", "medium", "synthetic_project")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			// Treat as valid only if it looks like a generated project.
			if _, err := os.Stat(filepath.Join(candidate, "cmd", "app", "main.go")); err == nil {
				mediumRoot = candidate
				return
			}
		}
		// Fallback: generate synthetic project into temp dir.
		out, err := os.MkdirTemp("", "orchestra-bench-medium-*")
		if err != nil {
			mediumErr = err
			return
		}
		mediumErr = generateSyntheticProject(out, genOpts{
			seed:        int64Env("ORCHESTRA_BENCH_SEED", 42),
			files:       intEnv("ORCHESTRA_BENCH_MEDIUM_FILES", 300),
			packages:    intEnv("ORCHESTRA_BENCH_MEDIUM_PACKAGES", 40),
			maxDepth:    intEnv("ORCHESTRA_BENCH_MEDIUM_MAX_DEPTH", 4),
			sizeProfile: strEnv("ORCHESTRA_BENCH_MEDIUM_SIZE_PROFILE", "medium"),
		})
		if mediumErr != nil {
			return
		}
		mediumRoot = out
	})
	return mediumRoot, mediumErr
}

func projectRootLarge() (string, error) {
	onceLarge.Do(func() {
		// Try real_project first (if exists locally)
		// Note: real_project contents are gitignored, so this only works if user placed it locally
		candidate := filepath.Join(repoRoot(), "testdata", "large", "real_project")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			// Check if it contains actual project (not just README)
			entries, err := os.ReadDir(candidate)
			if err == nil && len(entries) > 1 { // More than just README.md
				largeRoot = candidate
				return
			}
		}
		// Try synthetic_project (generated Go project)
		candidate = filepath.Join(repoRoot(), "testdata", "large", "synthetic_project")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			// Use repo-local generated project if present (handy for manual runs).
			if _, err := os.Stat(filepath.Join(candidate, "cmd", "app", "main.go")); err == nil {
				largeRoot = candidate
				return
			}
		}
		// Otherwise generate a large project into temp dir.
		out, err := os.MkdirTemp("", "orchestra-bench-large-*")
		if err != nil {
			largeErr = err
			return
		}
		largeErr = generateSyntheticProject(out, genOpts{
			seed:        int64Env("ORCHESTRA_BENCH_SEED", 42),
			files:       intEnv("ORCHESTRA_BENCH_LARGE_FILES", 2500),
			packages:    intEnv("ORCHESTRA_BENCH_LARGE_PACKAGES", 200),
			maxDepth:    intEnv("ORCHESTRA_BENCH_LARGE_MAX_DEPTH", 6),
			sizeProfile: strEnv("ORCHESTRA_BENCH_LARGE_SIZE_PROFILE", "small"),
		})
		if largeErr != nil {
			return
		}
		largeRoot = out
	})
	return largeRoot, largeErr
}

func benchLargeEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ORCHESTRA_BENCH_LARGE"))
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}

func intEnv(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func int64Env(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return i
}

func strEnv(key string, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func generateSyntheticProject(root string, opts genOpts) error {
	if opts.files <= 0 {
		return fmt.Errorf("files must be > 0")
	}
	if opts.packages <= 0 {
		return fmt.Errorf("packages must be > 0")
	}
	if opts.maxDepth <= 0 {
		opts.maxDepth = 1
	}

	// Base directories.
	baseDirs := []string{"cmd", "internal", "pkg", "api", "docs", "configs", "scripts"}
	for _, d := range baseDirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			return err
		}
	}

	// Ensure at least one "main.go" exists (common query target).
	if err := os.MkdirAll(filepath.Join(root, "cmd", "app"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "app", "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644); err != nil {
		return err
	}

	r := rand.New(rand.NewSource(opts.seed))

	pkgs := make([]string, 0, opts.packages)
	for i := 1; i <= opts.packages; i++ {
		pkgs = append(pkgs, fmt.Sprintf("pkg%03d", i))
	}

	seg := func() string {
		return fmt.Sprintf("d%02d", r.Intn(40))
	}
	pickPkg := func() string {
		return pkgs[r.Intn(len(pkgs))]
	}
	pickExt := func() string {
		// Mostly Go files to reflect real codebases.
		x := r.Intn(100)
		switch {
		case x < 65:
			return ".go"
		case x < 75:
			return ".md"
		case x < 85:
			return ".yaml"
		case x < 93:
			return ".json"
		case x < 97:
			return ".proto"
		default:
			return ".ts"
		}
	}

	targetSize := func() int {
		// Keep most files under daemon.DefaultMaxCacheFileBytes (64KB) for cache effect.
		switch strings.ToLower(strings.TrimSpace(opts.sizeProfile)) {
		case "small":
			return 300 + r.Intn(900) // 0.3–1.2KB
		case "medium":
			return 1200 + r.Intn(5000) // 1.2–6.2KB
		case "large":
			return 8*1024 + r.Intn(24*1024) // 8–32KB
		default:
			return 1200 + r.Intn(5000)
		}
	}

	write := func(relPath string, content string) error {
		p := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(content), 0644)
	}

	// Generate remaining files.
	for i := 1; i < opts.files; i++ {
		ext := pickExt()
		idx := i

		switch ext {
		case ".go":
			// Mix between cmd (main packages) and libraries.
			isCmd := r.Intn(100) < 10
			if isCmd {
				app := fmt.Sprintf("app%02d", 1+r.Intn(20))
				relDir := filepath.Join("cmd", app)
				name := fmt.Sprintf("main_%05d.go", idx)
				content := goMainFileContent(idx, targetSize())
				if err := write(filepath.Join(relDir, name), content); err != nil {
					return err
				}
				continue
			}

			pkgName := pickPkg()
			base := "internal"
			if r.Intn(100) < 35 {
				base = "pkg"
			}

			depth := 1 + r.Intn(opts.maxDepth)
			relDir := base
			for d := 0; d < depth-1; d++ {
				relDir = filepath.Join(relDir, seg())
			}
			relDir = filepath.Join(relDir, pkgName)

			name := fmt.Sprintf("file_%05d.go", idx)
			content := goLibFileContent(pkgName, idx, targetSize())
			if err := write(filepath.Join(relDir, name), content); err != nil {
				return err
			}

		case ".md":
			depth := 1 + r.Intn(opts.maxDepth)
			relDir := "docs"
			for d := 0; d < depth; d++ {
				relDir = filepath.Join(relDir, seg())
			}
			name := fmt.Sprintf("doc_%05d.md", idx)
			content := textFileContent("# Doc\n", idx, targetSize())
			if err := write(filepath.Join(relDir, name), content); err != nil {
				return err
			}

		case ".yaml":
			depth := 1 + r.Intn(opts.maxDepth)
			relDir := "configs"
			for d := 0; d < depth; d++ {
				relDir = filepath.Join(relDir, seg())
			}
			name := fmt.Sprintf("cfg_%05d.yaml", idx)
			content := textFileContent("kind: Config\n", idx, targetSize())
			if err := write(filepath.Join(relDir, name), content); err != nil {
				return err
			}

		case ".json":
			depth := 1 + r.Intn(opts.maxDepth)
			relDir := "api"
			for d := 0; d < depth; d++ {
				relDir = filepath.Join(relDir, seg())
			}
			name := fmt.Sprintf("data_%05d.json", idx)
			content := textFileContent("{\n  \"kind\": \"Data\",\n", idx, targetSize())
			if err := write(filepath.Join(relDir, name), content); err != nil {
				return err
			}

		case ".proto":
			depth := 1 + r.Intn(opts.maxDepth)
			relDir := "api"
			for d := 0; d < depth; d++ {
				relDir = filepath.Join(relDir, seg())
			}
			name := fmt.Sprintf("svc_%05d.proto", idx)
			content := protoFileContent(idx, targetSize())
			if err := write(filepath.Join(relDir, name), content); err != nil {
				return err
			}

		case ".ts":
			depth := 1 + r.Intn(opts.maxDepth)
			relDir := "scripts"
			for d := 0; d < depth; d++ {
				relDir = filepath.Join(relDir, seg())
			}
			name := fmt.Sprintf("tool_%05d.ts", idx)
			content := tsFileContent(idx, targetSize())
			if err := write(filepath.Join(relDir, name), content); err != nil {
				return err
			}
		}
	}

	return nil
}

func goMainFileContent(idx int, target int) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import \"fmt\"\n\n")
	fmt.Fprintf(&b, "func main() {\n\tfmt.Println(\"bench main %d logging\")\n}\n", idx)
	for b.Len() < target {
		fmt.Fprintf(&b, "// filler %d logging\n", idx)
	}
	return b.String()
}

func goLibFileContent(pkg string, idx int, target int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import \"fmt\"\n\n")
	b.WriteString("type Item struct {\n\tID int\n\tName string\n}\n\n")
	fmt.Fprintf(&b, "func Func%05d() string {\n\tfmt.Println(\"%s %d logging\")\n\treturn \"%s\"\n}\n", idx, pkg, idx, pkg)
	for b.Len() < target {
		fmt.Fprintf(&b, "// filler %s %d logging\n", pkg, idx)
	}
	return b.String()
}

func protoFileContent(idx int, target int) string {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\n\n")
	fmt.Fprintf(&b, "package api%d;\n\n", idx%97)
	fmt.Fprintf(&b, "message Msg%05d {\n  string id = 1;\n}\n", idx)
	for b.Len() < target {
		fmt.Fprintf(&b, "// filler %d\n", idx)
	}
	return b.String()
}

func tsFileContent(idx int, target int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "export function tool%05d(): string {\n  return 'logging';\n}\n", idx)
	for b.Len() < target {
		fmt.Fprintf(&b, "// filler %d\n", idx)
	}
	return b.String()
}

func textFileContent(prefix string, idx int, target int) string {
	var b strings.Builder
	b.WriteString(prefix)
	fmt.Fprintf(&b, "id: %d\n", idx)
	for b.Len() < target {
		fmt.Fprintf(&b, "- item_%d: logging\n", idx)
	}
	return b.String()
}
