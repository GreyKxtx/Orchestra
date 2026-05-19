// Package subproc provides shared helpers for managing language-server,
// MCP-server and core-child subprocesses: bounded stderr ring buffers
// and cross-platform process-tree cleanup. Extracted from lsp/, mcp/
// and cli/ in S1+S2 of the audit ledger (Sprint 6) to eliminate
// duplicated implementations.
package subproc

import (
	"io"
	"sync"
)

// DefaultRingSize is the per-subprocess stderr cap (64 KiB). Enough to
// capture a typical crash dump or npm install error without leaking
// memory for a server that logs forever.
const DefaultRingSize = 64 * 1024

// StderrRing is a goroutine-safe bounded ring buffer for subprocess
// stderr output. Append via Drain (typically launched as a goroutine);
// read the latest <=size bytes via Tail.
//
// The zero value behaves as if NewStderrRing(DefaultRingSize) was called
// — useful when a struct embeds *StderrRing and the parent's setup is
// best-effort.
type StderrRing struct {
	size int
	mu   sync.Mutex
	buf  []byte
}

// NewStderrRing creates a ring with the given cap. size <= 0 falls back
// to DefaultRingSize.
func NewStderrRing(size int) *StderrRing {
	if size <= 0 {
		size = DefaultRingSize
	}
	return &StderrRing{size: size}
}

// Drain reads r in a tight loop and keeps the most recent size bytes.
// Returns when r reports any read error (typically io.EOF when the
// subprocess closes its stderr pipe). Nil receiver and nil reader are
// no-ops.
func (s *StderrRing) Drain(r io.Reader) {
	if s == nil || r == nil {
		return
	}
	size := s.size
	if size <= 0 {
		size = DefaultRingSize
	}
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.buf = append(s.buf, buf[:n]...)
			if len(s.buf) > size {
				drop := len(s.buf) - size
				s.buf = s.buf[drop:]
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// Tail returns the last <=size bytes of stderr the subprocess wrote.
// Safe to call from any goroutine while Drain is running.
func (s *StderrRing) Tail() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}
