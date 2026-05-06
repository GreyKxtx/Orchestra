// Package lsptest provides a minimal mock LSP server for unit tests.
package lsptest

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync"

	"github.com/orchestra/orchestra/internal/lsp"
)

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// HandlerFunc handles an LSP request (id != nil) or notification (id == nil).
// For notifications the return value is ignored.
type HandlerFunc func(params json.RawMessage) (json.RawMessage, error)

// Server is a minimal in-process mock LSP server.
type Server struct {
	conn io.ReadWriteCloser
	r    *bufio.Reader
	wMu  sync.Mutex

	mu       sync.Mutex
	handlers map[string]HandlerFunc
}

// New starts a mock server that reads/writes to conn.
// Automatically responds to initialize (utf-8 posEncoding), initialized (no-op),
// shutdown (null result), and exit (close).
func New(conn io.ReadWriteCloser) *Server {
	s := &Server{
		conn:     conn,
		r:        bufio.NewReaderSize(conn, 64*1024),
		handlers: make(map[string]HandlerFunc),
	}
	s.handlers["initialize"] = func(_ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"capabilities":{"positionEncoding":"utf-8"}}`), nil
	}
	s.handlers["shutdown"] = func(_ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}
	go s.serve()
	return s
}

// NewConn returns a client-side net.Conn and a mock server attached to the other end.
// Pass the client conn to lsp.StartFromConn.
func NewConn() (net.Conn, *Server) {
	c1, c2 := net.Pipe()
	return c1, New(c2)
}

// SetHandler registers fn to handle the given LSP method.
func (s *Server) SetHandler(method string, fn HandlerFunc) {
	s.mu.Lock()
	s.handlers[method] = fn
	s.mu.Unlock()
}

// PushDiagnostics sends textDocument/publishDiagnostics to the connected client.
func (s *Server) PushDiagnostics(uri string, diags []lsp.Diagnostic) error {
	p, err := json.Marshal(map[string]any{"uri": uri, "diagnostics": diags})
	if err != nil {
		return err
	}
	msg := envelope{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Params: p}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.wMu.Lock()
	defer s.wMu.Unlock()
	return lsp.WriteMessage(s.conn, b)
}

func (s *Server) serve() {
	for {
		body, err := lsp.ReadMessage(s.r)
		if err != nil {
			return
		}
		var msg envelope
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		if msg.Method == "" {
			continue // ignore responses from client
		}

		s.mu.Lock()
		fn := s.handlers[msg.Method]
		s.mu.Unlock()

		if msg.ID == nil {
			// Notification — call handler, no response.
			if fn != nil {
				_, _ = fn(msg.Params)
			}
			continue
		}

		var reply envelope
		reply.JSONRPC = "2.0"
		reply.ID = msg.ID
		if fn != nil {
			result, err := fn(msg.Params)
			if err != nil {
				reply.Error = &rpcErr{Code: -32000, Message: err.Error()}
			} else {
				reply.Result = result
			}
		} else {
			reply.Result = json.RawMessage(`null`)
		}
		b, _ := json.Marshal(reply)
		s.wMu.Lock()
		_ = lsp.WriteMessage(s.conn, b)
		s.wMu.Unlock()
	}
}
