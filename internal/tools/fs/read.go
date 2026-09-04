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
		return nil, err
	}

	if strings.HasSuffix(relSlash, ".go") && c.Hooks.GoFileRedirect != nil {
		if redirect := c.Hooks.GoFileRedirect(ctx, relSlash, hash); redirect != "" {
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

// FormatGoFileRedirect builds a symbol-list response for .go files.
func FormatGoFileRedirect(relSlash, hash string, syms []GoSymbol) string {
	if len(syms) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Reading a .go file with read is wasteful — the file may be thousands of lines.\n")
	sb.WriteString("Use explore() to read the code. The file_hash below is for patching.\n\n")
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
