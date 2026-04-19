package jsonrpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// LSP-style framing for JSON-RPC over stdio:
//
// Content-Length: <bytes>\r\n
// \r\n
// <json payload bytes>

type Reader struct {
	r               *bufio.Reader
	maxContentBytes int
}

func NewReader(in io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(in), maxContentBytes: DefaultMaxContentBytes}
}

const (
	// DefaultMaxContentBytes caps Content-Length to avoid unbounded allocations.
	// This is intentionally conservative; adjust if you need large payloads.
	DefaultMaxContentBytes = 4 * 1024 * 1024 // 4MB
	maxHeaderLines         = 64
	maxHeaderLineBytes     = 8 * 1024
	maxHeaderBytes         = 32 * 1024
)

func (rd *Reader) ReadMessage() ([]byte, error) {
	if rd == nil || rd.r == nil {
		return nil, io.EOF
	}

	contentLength := -1
	lines := 0
	headerBytes := 0
	contentLengthSeen := false

	// Read headers.
	for {
		lineBytes, err := readLineLimited(rd.r, maxHeaderLineBytes)
		if err != nil {
			return nil, err
		}
		headerBytes += len(lineBytes)
		if headerBytes > maxHeaderBytes {
			return nil, fmt.Errorf("headers too large")
		}
		lines++
		if lines > maxHeaderLines {
			return nil, fmt.Errorf("too many header lines")
		}
		line := strings.TrimRight(string(lineBytes), "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		if key == "content-length" {
			if contentLengthSeen {
				return nil, fmt.Errorf("duplicate Content-Length")
			}
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length: %q", val)
			}
			contentLength = n
			contentLengthSeen = true
		}
	}

	if contentLength < 0 || !contentLengthSeen {
		return nil, fmt.Errorf("missing Content-Length")
	}
	maxLen := rd.maxContentBytes
	if maxLen <= 0 {
		maxLen = DefaultMaxContentBytes
	}
	if contentLength > maxLen {
		return nil, fmt.Errorf("Content-Length exceeds limit: %d > %d", contentLength, maxLen)
	}

	msg := make([]byte, contentLength)
	if _, err := io.ReadFull(rd.r, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func readLineLimited(r *bufio.Reader, maxBytes int) ([]byte, error) {
	if r == nil {
		return nil, io.EOF
	}
	if maxBytes <= 0 {
		maxBytes = maxHeaderLineBytes
	}

	var out []byte
	for {
		frag, err := r.ReadSlice('\n')
		out = append(out, frag...)
		if len(out) > maxBytes {
			return nil, fmt.Errorf("header line too long")
		}
		if err == nil {
			return out, nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return nil, err
	}
}

type Writer struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func NewWriter(out io.Writer) *Writer {
	return &Writer{w: bufio.NewWriter(out)}
}

func (wr *Writer) WriteMessage(payload any) error {
	if wr == nil || wr.w == nil {
		return io.ErrClosedPipe
	}
	wr.mu.Lock()
	defer wr.mu.Unlock()
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var header bytes.Buffer
	_, _ = fmt.Fprintf(&header, "Content-Length: %d\r\n\r\n", len(b))

	if _, err := wr.w.Write(header.Bytes()); err != nil {
		return err
	}
	if _, err := wr.w.Write(b); err != nil {
		return err
	}
	return wr.w.Flush()
}
