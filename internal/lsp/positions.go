package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// PathToURI converts an absolute filesystem path to a file:// URI.
//
// L6 in audit ledger: percent-encode reserved characters in each path
// segment so paths containing spaces, `#`, `?`, etc. produce a valid URI.
// Gopls tolerates unescaped spaces; some other servers don't.
func PathToURI(absPath string) string {
	slashPath := filepath.ToSlash(absPath)
	segments := strings.Split(slashPath, "/")
	for i, seg := range segments {
		// Skip the empty leading segment (rooted POSIX paths) and the
		// drive-letter segment on Windows.
		if seg == "" {
			continue
		}
		if runtime.GOOS == "windows" && i == 0 && len(seg) >= 2 && seg[1] == ':' {
			continue
		}
		segments[i] = url.PathEscape(seg)
	}
	encoded := strings.Join(segments, "/")
	if runtime.GOOS == "windows" && len(encoded) >= 2 && encoded[1] == ':' {
		return "file:///" + encoded
	}
	return "file://" + encoded
}

// URIToPath converts a file:// URI to an absolute filesystem path.
// Reverses PathToURI: percent-decode each segment.
func URIToPath(uri string) (string, error) {
	if !strings.HasPrefix(uri, "file://") {
		return "", fmt.Errorf("lsp: URI scheme is not file: %q", uri)
	}
	path := strings.TrimPrefix(uri, "file://")
	if runtime.GOOS == "windows" {
		// "///C:/path" → "C:/path"
		path = strings.TrimPrefix(path, "/")
	}
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	return filepath.FromSlash(path), nil
}

// ToolPosition is a 1-based (line, col) pair as used in Orchestra tool I/O.
type ToolPosition struct {
	Line int // 1-based
	Col  int // 1-based byte offset
}

// ToLSP converts a 1-based ToolPosition to a 0-based LSP Position.
// posEncoding should be "utf-8" or "utf-16". lineText is needed only for UTF-16 conversion.
func (tp ToolPosition) ToLSP(posEncoding, lineText string) Position {
	line := tp.Line - 1
	if line < 0 {
		line = 0
	}
	col := tp.Col - 1
	if col < 0 {
		col = 0
	}
	if posEncoding == "utf-16" && col > 0 && lineText != "" {
		col = byteToUTF16Offset(lineText, col)
	}
	return Position{Line: uint32(line), Character: uint32(col)}
}

// ToolPositionFrom converts a 0-based LSP Position to a 1-based ToolPosition.
func ToolPositionFrom(pos Position, posEncoding, lineText string) ToolPosition {
	col := int(pos.Character)
	if posEncoding == "utf-16" && col > 0 && lineText != "" {
		col = utf16ToByteOffset(lineText, col)
	}
	return ToolPosition{Line: int(pos.Line) + 1, Col: col + 1}
}

// byteToUTF16Offset converts a byte offset in s to a UTF-16 code unit count.
func byteToUTF16Offset(s string, byteOffset int) int {
	if byteOffset >= len(s) {
		return countUTF16Units(s)
	}
	return countUTF16Units(s[:byteOffset])
}

// utf16ToByteOffset converts a UTF-16 code unit offset to a byte offset in s.
func utf16ToByteOffset(s string, utf16Offset int) int {
	units := 0
	for i, r := range s {
		if units >= utf16Offset {
			return i
		}
		if r >= 0x10000 {
			units += 2 // surrogate pair
		} else {
			units++
		}
	}
	return len(s)
}

// countUTF16Units counts UTF-16 code units needed to encode s.
func countUTF16Units(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
