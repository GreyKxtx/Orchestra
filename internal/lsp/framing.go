package lsp

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ReadMessage reads one LSP message from r using Content-Length framing.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("lsp: read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length: "))
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("lsp: invalid Content-Length %q: %w", val, err)
			}
			contentLength = n
		}
		// Other headers (Content-Type) are silently ignored.
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("lsp: missing Content-Length header")
	}
	if contentLength == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("lsp: read body: %w", err)
	}
	return buf, nil
}

// WriteMessage writes body with Content-Length framing to w.
func WriteMessage(w io.Writer, body []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(w, header); err != nil {
		return fmt.Errorf("lsp: write header: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("lsp: write body: %w", err)
	}
	return nil
}
