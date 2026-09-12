// Package repomap builds a fast, cold-start outline of a project — a compact
// "table of contents" of every parseable source file (top-level functions,
// types, methods). Intended for one-shot injection into the LLM context when
// the agent hasn't built (or doesn't need) a full CKG index.
//
// Reuses internal/ckg.ParseFile for tree-sitter parsing across all CKG-supported
// languages, but does not require the SQLite store. Output is token-budgeted:
// when the requested budget is too small for everything, files with the fewest
// definitions are pruned first, then private symbols inside the remaining files.
package repomap

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/ckg"
)

// FileOutline is the extracted outline of one source file.
type FileOutline struct {
	Path    string // workspace-relative, forward-slash form
	Lang    string // language id (matches ckg.LanguageFromExt)
	Symbols []Symbol
}

// Symbol is one top-level definition (package excluded).
type Symbol struct {
	Name    string // short name; methods use "Receiver.Name" form
	Kind    string // "func" | "method" | "struct" | "interface" | "type" | "package"
	Line    int    // 1-based
	Private bool   // true when first rune is lowercase (Go convention) — used by pruning
}

// RepoMap is the aggregate outline of a workspace.
type RepoMap struct {
	Root    string
	Files   []FileOutline
	Skipped int // count of source files that parsed empty / had no supported lang
}

// Options controls the walk and the formatting.
type Options struct {
	// ExcludeDirs is a list of directory basenames to skip (in addition to
	// the always-skip defaults: .git, node_modules, vendor, dist, build,
	// .orchestra). Matched against the basename of each directory.
	ExcludeDirs []string
	// MaxFiles is a safety cap on how many files to parse. 0 = no cap.
	MaxFiles int
	// MaxFileBytes skips files larger than this. 0 = no cap. Default 512 KiB
	// when the caller passes 0 to keep cold-start fast on big generated files.
	MaxFileBytes int64
}

// defaultIgnores are always skipped during the walk.
var defaultIgnores = map[string]bool{
	".git":        true,
	"node_modules": true,
	"vendor":      true,
	"dist":        true,
	"build":       true,
	".orchestra":  true,
	".idea":       true,
	".vscode":     true,
	"target":      true, // Rust
}

// Build walks root and returns an outline of every parseable file it finds.
// Honours context cancellation between files (per-file parsing is short).
func Build(ctx context.Context, root string, opts Options) (*RepoMap, error) {
	if root == "" {
		return nil, fmt.Errorf("repomap: root is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("repomap: abs %s: %w", root, err)
	}

	if opts.MaxFileBytes == 0 {
		opts.MaxFileBytes = 512 * 1024
	}

	extraIgnores := make(map[string]bool, len(opts.ExcludeDirs))
	for _, d := range opts.ExcludeDirs {
		extraIgnores[filepath.Base(d)] = true
	}

	out := &RepoMap{Root: absRoot}

	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if path != absRoot && (defaultIgnores[base] || extraIgnores[base] || strings.HasPrefix(base, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		lang := ckg.LanguageFromExt(ext)
		if lang == "unknown" {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
			out.Skipped++
			return nil
		}
		if opts.MaxFiles > 0 && len(out.Files) >= opts.MaxFiles {
			return filepath.SkipDir
		}

		fo, perr := outlineFile(ctx, absRoot, path, lang)
		if perr != nil || fo == nil || len(fo.Symbols) == 0 {
			if perr != nil || fo == nil {
				out.Skipped++
			}
			return nil
		}
		out.Files = append(out.Files, *fo)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	return out, nil
}

// outlineFile parses one file via ckg.ParseFile and extracts a symbol list.
// modulePath is passed empty; CKG's FQN resolution still works for the outline
// (we only use ShortName + Kind + LineStart).
func outlineFile(ctx context.Context, root, path, lang string) (*FileOutline, error) {
	nodes, _, _, err := ckg.ParseFile(ctx, "", root, path)
	if err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	fo := &FileOutline{Path: rel, Lang: lang}
	for _, n := range nodes {
		// Drop the synthetic package node; it's redundant with the file path.
		if n.Kind == "package" {
			continue
		}
		short := n.ShortName
		if short == "" {
			continue
		}
		fo.Symbols = append(fo.Symbols, Symbol{
			Name:    short,
			Kind:    n.Kind,
			Line:    n.LineStart,
			Private: isPrivate(short),
		})
	}
	sort.Slice(fo.Symbols, func(i, j int) bool { return fo.Symbols[i].Line < fo.Symbols[j].Line })
	return fo, nil
}

func isPrivate(name string) bool {
	if name == "" {
		return false
	}
	// Strip receiver prefix ("Type.Method" → "Method") for visibility classification.
	if i := strings.LastIndexByte(name, '.'); i >= 0 && i+1 < len(name) {
		name = name[i+1:]
	}
	r := []rune(name)[0]
	return r >= 'a' && r <= 'z'
}

// Format renders the repo map as compact text. When budgetBytes > 0, the
// output is trimmed to fit by:
//  1. dropping private symbols inside files with > 6 symbols;
//  2. dropping whole files with the fewest symbols;
//  3. emitting a trailing "(N more files omitted)" line when truncation occurred.
// The output is stable for a given input.
func Format(rm *RepoMap, budgetBytes int) string {
	if rm == nil || len(rm.Files) == 0 {
		return "(empty repo map)\n"
	}

	// Layer 1: full output.
	full := renderFiles(rm.Files)
	if budgetBytes <= 0 || len(full) <= budgetBytes {
		return full
	}

	// Layer 2: drop private symbols inside files with > 6 symbols.
	pruned := make([]FileOutline, 0, len(rm.Files))
	for _, f := range rm.Files {
		if len(f.Symbols) <= 6 {
			pruned = append(pruned, f)
			continue
		}
		kept := make([]Symbol, 0, len(f.Symbols))
		for _, s := range f.Symbols {
			if s.Private {
				continue
			}
			kept = append(kept, s)
		}
		// If we pruned everything, keep at least one signal symbol.
		if len(kept) == 0 {
			kept = f.Symbols[:1]
		}
		pruned = append(pruned, FileOutline{Path: f.Path, Lang: f.Lang, Symbols: kept})
	}
	text := renderFiles(pruned)
	if len(text) <= budgetBytes {
		return text
	}

	// Layer 3: the shape of the repository.
	//
	// Layer 4 below drops the smallest files first, which keeps the single
	// largest outline and omits everything else — for a 3 KB budget on a
	// thousand-file repo that answers "what is inside this one file" when the
	// question was "what is this project". Once the outline overruns the
	// budget by a wide margin, a digest of directories — how many files and
	// symbols each holds — is the honest map, and it fits any repo.
	if len(text) > budgetBytes*digestOverflowFactor {
		if digest := renderDirDigest(rm, budgetBytes); digest != "" {
			return digest
		}
	}

	// Layer 4: greedy drop of smallest files until we fit. Iteration is bounded
	// by len(pruned), so worst case is O(n²) in file count — acceptable for
	// budgets that ever require this much trimming (rare).
	sortedBySize := append([]FileOutline(nil), pruned...)
	sort.SliceStable(sortedBySize, func(i, j int) bool { return len(sortedBySize[i].Symbols) < len(sortedBySize[j].Symbols) })
	dropped := 0
	keepSet := make(map[string]bool, len(sortedBySize))
	for _, f := range sortedBySize {
		keepSet[f.Path] = true
	}
	for i := 0; i < len(sortedBySize); i++ {
		// Render only kept files in original sorted-by-path order.
		filtered := make([]FileOutline, 0, len(pruned))
		for _, f := range pruned {
			if keepSet[f.Path] {
				filtered = append(filtered, f)
			}
		}
		body := renderFiles(filtered)
		tail := ""
		if dropped > 0 {
			tail = fmt.Sprintf("\n(%d more file%s omitted to fit budget)\n", dropped, plural(dropped))
		}
		if len(body)+len(tail) <= budgetBytes {
			return body + tail
		}
		// Drop the next-smallest file and retry.
		keepSet[sortedBySize[i].Path] = false
		dropped++
	}
	// Nothing fits; return the bare omission line so the caller knows.
	return fmt.Sprintf("(%d files; %d bytes budget too small for any outline)\n", len(rm.Files), budgetBytes)
}

// digestOverflowFactor is how far the per-file outline may overrun the budget
// before the directory digest replaces it. Below it, dropping files keeps a
// map that is still a map; above it, what survives is not representative.
const digestOverflowFactor = 2

// renderDirDigest is the repository at directory granularity: one line per
// directory with how many files and symbols it holds, biggest first, and a
// pointer to what to call next. Returns "" when even that does not fit, so the
// caller can fall through to dropping files.
func renderDirDigest(rm *RepoMap, budgetBytes int) string {
	type dirStat struct {
		dir     string
		files   int
		symbols int
	}
	byDir := make(map[string]*dirStat, 32)
	totalSymbols := 0
	for _, f := range rm.Files {
		dir := path.Dir(filepath.ToSlash(f.Path))
		if dir == "." || dir == "/" {
			dir = "(root)"
		}
		d := byDir[dir]
		if d == nil {
			d = &dirStat{dir: dir}
			byDir[dir] = d
		}
		d.files++
		d.symbols += len(f.Symbols)
		totalSymbols += len(f.Symbols)
	}
	dirs := make([]*dirStat, 0, len(byDir))
	for _, d := range byDir {
		dirs = append(dirs, d)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i].symbols != dirs[j].symbols {
			return dirs[i].symbols > dirs[j].symbols
		}
		return dirs[i].dir < dirs[j].dir
	})

	head := fmt.Sprintf("repo: %d files · %d symbols · %d dirs (shape only — the outline needs a bigger budget_bytes):\n",
		len(rm.Files), totalSymbols, len(dirs))
	const tailFmt = "(+%d more dir%s; explore(\"<dir>\") for its symbols)\n"
	tailAll := "(explore(\"<dir>\") for a directory's symbols)\n"
	if len(head)+len(tailAll) > budgetBytes {
		return ""
	}

	var b strings.Builder
	b.WriteString(head)
	shown := 0
	for _, d := range dirs {
		line := fmt.Sprintf("  %s — %d file%s, %d symbol%s\n", d.dir, d.files, plural(d.files), d.symbols, plural(d.symbols))
		left := len(dirs) - shown - 1
		tail := tailAll
		if left > 0 {
			tail = fmt.Sprintf(tailFmt, left, dirPlural(left))
		}
		if b.Len()+len(line)+len(tail) > budgetBytes {
			break
		}
		b.WriteString(line)
		shown++
	}
	if shown == 0 {
		return ""
	}
	if left := len(dirs) - shown; left > 0 {
		b.WriteString(fmt.Sprintf(tailFmt, left, dirPlural(left)))
	} else {
		b.WriteString(tailAll)
	}
	return b.String()
}

func dirPlural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func renderFiles(files []FileOutline) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f.Path)
		b.WriteByte('\n')
		for _, s := range f.Symbols {
			b.WriteString("  ")
			b.WriteString(s.Kind)
			b.WriteByte(' ')
			b.WriteString(s.Name)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// BuildAndFormat is a convenience wrapper combining Build + Format. Errors from
// Build propagate; missing-root is returned as os.ErrNotExist.
func BuildAndFormat(ctx context.Context, root string, opts Options, budgetBytes int) (string, error) {
	if _, err := os.Stat(root); err != nil {
		return "", err
	}
	rm, err := Build(ctx, root, opts)
	if err != nil {
		return "", err
	}
	return Format(rm, budgetBytes), nil
}
