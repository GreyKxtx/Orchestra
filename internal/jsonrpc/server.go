package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/protocol"
)

// Handler dispatches JSON-RPC methods.
type Handler interface {
	Handle(ctx context.Context, method string, params json.RawMessage) (any, error)
}

type Server struct {
	h Handler
	r *Reader
	w *Writer

	pMu     sync.Mutex
	nextID  int
	pending map[string]chan clientReply

	// inFlightMu guards inFlight, which maps a normalised in-flight client
	// request id ("42" or `"abc"` with quotes stripped) to its dispatch ctx
	// CancelFunc. Populated when Serve dispatches a non-notification request,
	// drained when Handle returns. A `$/cancelRequest` notification looks up
	// the id here and calls the cancel.
	inFlightMu sync.Mutex
	inFlight   map[string]context.CancelFunc

	// dispatchWG tracks goroutines spawned for non-notification requests.
	// Serve waits on it before returning (EOF or ctx done) so callers that
	// drive Serve to completion with a finite input still see every response
	// written before Serve exits.
	dispatchWG sync.WaitGroup
}

type clientReply struct {
	result json.RawMessage
	err    error
}

// srvWireMsg is used to detect client responses to server-initiated requests.
type srvWireMsg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

func NewServer(h Handler, in io.Reader, out io.Writer) *Server {
	return &Server{
		h:        h,
		r:        NewReader(in),
		w:        NewWriter(out),
		pending:  make(map[string]chan clientReply),
		inFlight: make(map[string]context.CancelFunc),
	}
}

// normalizeID renders a JSON-RPC id RawMessage to a canonical map key:
// strings lose their quotes ("42" -> 42, `"abc"` -> abc), numbers keep
// their textual form. Used by both the client-response interceptor and the
// $/cancelRequest handler so they look up the same id under the same key.
func normalizeID(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func (s *Server) Serve(ctx context.Context) error {
	if s == nil {
		return nil
	}
	// Wait for any in-flight Handle goroutines before returning, so callers
	// driving Serve to completion with a finite input still observe every
	// response that the spawned dispatcher wrote.
	defer s.dispatchWG.Wait()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := s.r.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			// Parse error: respond with id=null.
			_ = s.w.WriteMessage(Response{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error: &Error{
					Code:    -32700,
					Message: "Parse error",
					Data:    map[string]any{"error": err.Error()},
				},
			})
			continue
		}

		// Intercept client responses to server-initiated requests (no method field + non-null id).
		var probe srvWireMsg
		if json.Unmarshal(msg, &probe) == nil && probe.Method == "" && len(probe.ID) > 0 && string(probe.ID) != "null" {
			id := normalizeID(probe.ID)
			s.pMu.Lock()
			ch, ok := s.pending[id]
			if ok {
				delete(s.pending, id)
			}
			s.pMu.Unlock()
			if ok {
				if probe.Error != nil {
					ch <- clientReply{err: &RPCError{Code: probe.Error.Code, Message: probe.Error.Message, Data: probe.Error.Data}}
				} else {
					ch <- clientReply{result: probe.Result}
				}
				continue
			}
			// Stale or unknown response — fall through to parsePayload (will produce an error, which is fine).
		}

		req, perr := parsePayload(msg)
		if perr != nil {
			_ = s.w.WriteMessage(Response{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error: &Error{
					Code:    perr.Code,
					Message: perr.Message,
					Data:    perr.Data,
				},
			})
			continue
		}

		// Notifications: no response.
		if req.IsNotification {
			// $/cancelRequest is consumed by the server itself — it cancels the
			// per-request context for an in-flight call so handlers that honour
			// ctx (agent.run, workflow.run, skill.invoke, agent loop tools)
			// unwind promptly. Unknown ids are silently ignored, matching LSP.
			if req.Method == "$/cancelRequest" {
				s.handleCancelRequest(req.Params)
				continue
			}
			_, _ = s.h.Handle(ctx, req.Method, req.Params)
			continue
		}

		// Per-request cancellable ctx — registered before dispatch so a
		// $/cancelRequest arriving mid-call can cancel it, deregistered
		// after dispatch so a late cancel notification is a no-op.
		//
		// Handle runs in its own goroutine so the Serve loop keeps reading.
		// Without this, a long-running call (agent.run for minutes) would
		// block the reader and `$/cancelRequest` would sit in the pipe
		// until the call finishes — defeating the entire point of
		// cancellation. Writer is mutex-protected, so concurrent response
		// writes from multiple in-flight handlers are safe.
		reqCtx, cancel := context.WithCancel(ctx)
		idKey := normalizeID(req.ID)
		s.inFlightMu.Lock()
		s.inFlight[idKey] = cancel
		s.inFlightMu.Unlock()

		s.dispatchWG.Add(1)
		go func(req parsedRequest, reqCtx context.Context, cancel context.CancelFunc, idKey string) {
			defer s.dispatchWG.Done()
			res, callErr := s.h.Handle(reqCtx, req.Method, req.Params)

			s.inFlightMu.Lock()
			delete(s.inFlight, idKey)
			s.inFlightMu.Unlock()
			cancel()

			if callErr != nil {
				_ = s.w.WriteMessage(Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   toRPCError(callErr),
				})
				return
			}
			_ = s.w.WriteMessage(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  res,
			})
		}(req, reqCtx, cancel, idKey)
	}
}

// handleCancelRequest interprets the params of `$/cancelRequest` and cancels
// the matching in-flight request's context. The id field accepts the same
// shape (string or number) the server sees on the wire; missing/unknown ids
// are silently ignored, matching LSP semantics.
func (s *Server) handleCancelRequest(params json.RawMessage) {
	var p struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil || len(p.ID) == 0 {
		return
	}
	idKey := normalizeID(p.ID)
	s.inFlightMu.Lock()
	cancel, ok := s.inFlight[idKey]
	if ok {
		delete(s.inFlight, idKey)
	}
	s.inFlightMu.Unlock()
	if ok {
		cancel()
	}
}

// Notify sends a server-initiated JSON-RPC notification (no id field) to the client.
// It is safe to call concurrently with Serve; the Writer is mutex-protected.
func (s *Server) Notify(method string, params any) error {
	if s == nil {
		return nil
	}
	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("notify marshal params: %w", err)
		}
		paramsRaw = b
	}
	return s.w.WriteMessage(Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
		// no ID = notification per JSON-RPC 2.0 spec
	})
}

// Request sends a server-initiated JSON-RPC request and waits for the client's response.
// Safe to call concurrently with Serve.
func (s *Server) Request(ctx context.Context, method string, params any, result any) error {
	if s == nil {
		return fmt.Errorf("jsonrpc: server is nil")
	}
	s.pMu.Lock()
	s.nextID++
	id := fmt.Sprintf("srv-%d", s.nextID)
	ch := make(chan clientReply, 1)
	s.pending[id] = ch
	s.pMu.Unlock()

	removePending := func() {
		s.pMu.Lock()
		delete(s.pending, id)
		s.pMu.Unlock()
	}

	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			removePending()
			return fmt.Errorf("request marshal params: %w", err)
		}
		paramsRaw = b
	}

	idJSON, _ := json.Marshal(id)
	req := Request{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
		Params:  paramsRaw,
	}
	if err := s.w.WriteMessage(req); err != nil {
		removePending()
		return err
	}

	select {
	case <-ctx.Done():
		removePending()
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if result != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, result)
		}
		return nil
	}
}

func classifyID(id json.RawMessage) (isNotification bool, valid bool) {
	// Notifications: absent id only.
	if len(id) == 0 {
		return true, true
	}
	var v any
	if err := json.Unmarshal(id, &v); err != nil {
		return false, false
	}
	switch v.(type) {
	case nil:
		return false, true
	case string:
		return false, true
	case float64:
		return false, true
	default:
		return false, false
	}
}

func toRPCError(err error) *Error {
	if err == nil {
		return nil
	}
	var re *RPCError
	if errors.As(err, &re) && re != nil {
		return &Error{
			Code:    re.Code,
			Message: re.Message,
			Data:    re.Data,
		}
	}
	if pe, ok := protocol.AsError(err); ok {
		return &Error{
			Code:    pe.Code.RPCCode(),
			Message: pe.Message,
			Data: map[string]any{
				"code": pe.Code,
				"data": pe.Data,
			},
		}
	}
	// Include error details in Data for debugging
	errMsg := err.Error()
	if len(errMsg) > 500 {
		errMsg = errMsg[:500] + "...(truncated)"
	}
	return &Error{
		Code:    -32603,
		Message: "Internal error",
		Data:    map[string]any{"error": errMsg, "error_type": fmt.Sprintf("%T", err)},
	}
}
