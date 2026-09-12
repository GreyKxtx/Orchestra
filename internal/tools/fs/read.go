package fs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

func (c *Client) Read(ctx context.Context, req FSReadRequest) (*FSReadResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}
	_ = ctx

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	absPath, relSlash, err := resolveWorkspacePath(c.Root, path)
	if err != nil {
		return nil, err
	}

	if c.isDryRun() && c.Overlay != nil {
		if stagedContent, stagedHash, ok := c.Overlay.stagedContent(relSlash); ok {
			numbered := addLineNumbers(stagedContent)
			return &FSReadResponse{
				Path:      relSlash,
				Content:   numbered,
				SHA256:    stagedHash,
				FileHash:  stagedHash,
				MTimeUnix: 0,
				Size:      int64(len(stagedContent)),
			}, nil
		}
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 200 * 1024
	}

	content, size, mtimeUnix, hash, truncated, err := readFileWithHash(absPath, maxBytes)
	if err != nil {
		if missing := missingPathError(c.Root, relSlash, err); missing != nil {
			return nil, missing
		}
		return nil, err
	}

	// req.MaxBytes, not the defaulted maxBytes: asking for a fixed number of
	// bytes is asking for the bytes. The model that broke item.go tried
	// max_bytes 200 to get round the redirect and got the redirect again.
	askedForBytes := req.MaxBytes > 0
	if strings.HasSuffix(relSlash, ".go") && c.Hooks.GoFileRedirect != nil &&
		!askedForBytes && goFileIsLongEnoughToRedirect(content) {
		if redirect := c.Hooks.GoFileRedirect(ctx, relSlash, hash); redirect != "" {
			// The package clause is the one line a whole-file write must
			// reproduce and the only one the symbol list cannot carry: the
			// index stores symbols, not the file's own header, and every
			// other tool names a Go symbol by import path. Those differ for
			// `package main` at a module root, and a model with only the
			// import path in front of it writes the import path.
			if pkg := goPackageClause(content); pkg != "" {
				redirect = pkg + "\n\n" + redirect
			}
			return &FSReadResponse{
				Path:      relSlash,
				Content:   redirect,
				SHA256:    hash,
				FileHash:  hash,
				MTimeUnix: mtimeUnix,
				Size:      size,
				Truncated: false,
			}, nil
		}
	}

	numbered := addLineNumbers(content)
	if c.Hooks.DiscoverInstructions != nil {
		if reminder := c.Hooks.DiscoverInstructions(filepath.Dir(absPath)); reminder != "" {
			numbered = "<system-reminder>\n" + reminder + "\n</system-reminder>\n\n" + numbered
		}
	}

	return &FSReadResponse{
		Path:      relSlash,
		Content:   numbered,
		SHA256:    hash,
		FileHash:  hash,
		MTimeUnix: mtimeUnix,
		Size:      size,
		Truncated: truncated,
	}, nil
}

// goRedirectMinLines is how long a .go file has to be before answering with a
// symbol list beats answering with the file.
//
// The redirect exists to keep a thousand-line file out of the context window,
// and below this its own premise is false: a 91-byte item.go was redirected
// with "the file may be thousands of lines", and since the redirect replaces
// the content there was then no way to see the file at all — max_bytes does
// not help, because the redirect is chosen before any truncation. The model
// wrote the file back with the wrong package clause and broke the build.
const goRedirectMinLines = 200

// goFileIsLongEnoughToRedirect reports whether a symbol list saves enough to
// be worth withholding the file.
func goFileIsLongEnoughToRedirect(content string) bool {
	return strings.Count(content, "\n") >= goRedirectMinLines
}

// goPackageClause returns the file's `package X` declaration, or "" when it
// has none. Scanning rather than parsing: the clause is the first line that
// is not blank, a comment or inside one, which is cheap and exact enough to
// quote back.
func goPackageClause(content string) string {
	inBlockComment := false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if inBlockComment {
			if i := strings.Index(line, "*/"); i >= 0 {
				line = strings.TrimSpace(line[i+2:])
				inBlockComment = false
			} else {
				continue
			}
		}
		for strings.HasPrefix(line, "/*") {
			end := strings.Index(line, "*/")
			if end < 0 {
				inBlockComment = true
				line = ""
				break
			}
			line = strings.TrimSpace(line[end+2:])
		}
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "package ") {
			return line
		}
		// The first real line was not a package clause, so there is none.
		return ""
	}
	return ""
}

// FormatGoFileRedirect builds a symbol-list response for .go files.
func FormatGoFileRedirect(relSlash, hash string, syms []GoSymbol) string {
	if len(syms) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("This .go file is long, so here are its symbols instead of its text.\n")
	sb.WriteString("Use explore(\"Name\") for one symbol's code, or read again with max_bytes\n")
	sb.WriteString("for the file itself. The file_hash below is for patching.\n\n")
	sb.WriteString("file_hash: " + hash + "\n\n")
	sb.WriteString("Symbols in " + relSlash + ":\n")
	for _, s := range syms {
		sb.WriteString(fmt.Sprintf("  • %s (%s, lines %d-%d) → explore(\"%s\")\n",
			s.ShortName, s.Kind, s.LineStart, s.LineEnd, s.ShortName))
	}
	sb.WriteString("\nFor the exact code of one symbol, call explore(\"SymbolName\").\n")
	return sb.String()
}

// GoSymbol is a minimal symbol descriptor for read redirect text.
type GoSymbol struct {
	ShortName string
	Kind      string
	LineStart int
	LineEnd   int
}
