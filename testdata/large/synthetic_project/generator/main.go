package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

type options struct {
	seed        int64
	files       int
	packages    int
	maxDepth    int
	sizeProfile string
	out         string
	clean       bool
}

func main() {
	var opts options
	flag.Int64Var(&opts.seed, "seed", 0, "Random seed (required)")
	flag.IntVar(&opts.files, "files", 2500, "Total files to generate")
	flag.IntVar(&opts.packages, "packages", 200, "Number of Go packages")
	flag.IntVar(&opts.maxDepth, "max-depth", 6, "Max directory depth")
	flag.StringVar(&opts.sizeProfile, "size-profile", "small", "File size profile: small|medium|large")
	flag.StringVar(&opts.out, "out", filepath.Join("testdata", "large"), "Output directory")
	flag.BoolVar(&opts.clean, "clean", false, "Remove generated directories before generating")
	flag.Parse()

	if opts.seed == 0 {
		fmt.Fprintln(os.Stderr, "ERROR: --seed is required (use a non-zero int64)")
		os.Exit(2)
	}
	if opts.files <= 0 {
		fmt.Fprintln(os.Stderr, "ERROR: --files must be > 0")
		os.Exit(2)
	}
	if opts.packages <= 0 {
		fmt.Fprintln(os.Stderr, "ERROR: --packages must be > 0")
		os.Exit(2)
	}
	if opts.maxDepth <= 0 {
		opts.maxDepth = 1
	}

	outAbs, err := filepath.Abs(opts.out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: invalid --out: %v\n", err)
		os.Exit(2)
	}
	opts.out = outAbs

	if err := os.MkdirAll(opts.out, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to create output dir: %v\n", err)
		os.Exit(1)
	}

	if opts.clean {
		if err := cleanGenerated(opts.out); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: clean failed: %v\n", err)
			os.Exit(1)
		}
	}

	if err := generateProject(opts.out, opts); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: generate failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %d files into %s\n", opts.files, opts.out)
}

func cleanGenerated(out string) error {
	// Only remove directories we generate.
	for _, d := range []string{"cmd", "internal", "pkg", "api", "docs", "configs", "scripts"} {
		p := filepath.Join(out, d)
		_ = os.RemoveAll(p)
	}
	return nil
}

func generateProject(out string, opts options) error {
	// Base directories.
	baseDirs := []string{"cmd", "internal", "pkg", "api", "docs", "configs", "scripts"}
	for _, d := range baseDirs {
		if err := os.MkdirAll(filepath.Join(out, d), 0755); err != nil {
			return err
		}
	}

	// Ensure at least one "main.go" exists.
	if err := os.MkdirAll(filepath.Join(out, "cmd", "app"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "cmd", "app", "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644); err != nil {
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
		p := filepath.Join(out, relPath)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(content), 0644)
	}

	for i := 1; i < opts.files; i++ {
		ext := pickExt()
		idx := i

		switch ext {
		case ".go":
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
