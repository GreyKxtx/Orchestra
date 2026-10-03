package fs

import (
	"bytes"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A model writes "\n" whatever the project uses. Written as it came, a new
// file in a CRLF project was the odd one out, and a rewrite of a CRLF file
// changed every line. write fits the content to the file it replaces, or, for
// a new file, to the files beside it.

const (
	// lineEndingSampleFiles and lineEndingSampleBytes bound the look at the
	// neighbours: a few files, the start of each.
	lineEndingSampleFiles = 8
	lineEndingSampleBytes = 4096
)

// fitLineEndings returns content with the line endings of the file it
// replaces (current, when exists), or else of its neighbours. Content without a
// line break, or a project that has no clear convention, is left as it came.
func (c *Client) fitLineEndings(relSlash, content string, current string, exists bool) string {
	if !strings.Contains(content, "\n") {
		return content
	}
	// A script is LF whatever sits beside it: bash reads a CRLF script as
	// "\r: command not found".
	if !exists && isUnixScript(relSlash, content) {
		return strings.ReplaceAll(content, "\r\n", "\n")
	}
	var crlf, lf int
	if exists {
		crlf, lf = countLineEndings([]byte(current))
	} else {
		crlf, lf = c.neighbourLineEndings(relSlash)
	}
	switch {
	case crlf > lf:
		return strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\n", "\r\n")
	case lf > crlf && strings.Contains(content, "\r\n"):
		return strings.ReplaceAll(content, "\r\n", "\n")
	}
	return content
}

// isUnixScript: a shell script by its name, or any script by its shebang.
func isUnixScript(relSlash, content string) bool {
	switch strings.ToLower(path.Ext(relSlash)) {
	case ".sh", ".bash", ".zsh":
		return true
	}
	return strings.HasPrefix(content, "#!")
}

// neighbourLineEndings counts the line endings of text files beside relSlash
// on disk — in its directory, or the nearest one above it that has any. Files
// of its own kind decide when there are any: a .go file follows the .go files,
// not the .bat files next to them.
func (c *Client) neighbourLineEndings(relSlash string) (crlf, lf int) {
	dir := path.Dir(relSlash)
	ext := strings.ToLower(path.Ext(relSlash))
	for {
		abs := filepath.Join(c.Root, filepath.FromSlash(dir))
		if ext != "" {
			if crlf, lf = sampleDirLineEndings(abs, path.Base(relSlash), ext); crlf+lf > 0 {
				return crlf, lf
			}
		}
		crlf, lf = sampleDirLineEndings(abs, path.Base(relSlash), "")
		if crlf+lf > 0 || dir == "." || dir == "/" || dir == "" {
			return crlf, lf
		}
		dir = path.Dir(dir)
	}
}

// sampleDirLineEndings votes the text files in dir, those ending in ext when
// ext is set.
func sampleDirLineEndings(dir, skip, ext string) (crlf, lf int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	sampled := 0
	for _, e := range entries {
		if sampled >= lineEndingSampleFiles {
			break
		}
		if !e.Type().IsRegular() || e.Name() == skip || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if ext != "" && strings.ToLower(filepath.Ext(e.Name())) != ext {
			continue
		}
		head, err := readHead(filepath.Join(dir, e.Name()), lineEndingSampleBytes)
		if err != nil || bytes.IndexByte(head, 0) >= 0 {
			continue // unreadable or binary
		}
		c, l := countLineEndings(head)
		if c+l == 0 {
			continue
		}
		sampled++
		// One vote per file: a long LF file must not outvote three CRLF ones.
		if c > l {
			crlf++
		} else {
			lf++
		}
	}
	return crlf, lf
}

func readHead(p string, n int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

// countLineEndings counts CRLF and bare LF line breaks.
func countLineEndings(b []byte) (crlf, lf int) {
	for i, ch := range b {
		if ch != '\n' {
			continue
		}
		if i > 0 && b[i-1] == '\r' {
			crlf++
		} else {
			lf++
		}
	}
	return crlf, lf
}
