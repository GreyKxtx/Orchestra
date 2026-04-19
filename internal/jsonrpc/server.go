package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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
}

func NewServer(h Handler, in io.Reader, out io.Writer) *Server {
	return &Server{
		h: h,
		r: NewReader(in),
		w: NewWriter(out),
	}
}

func (s *Server) Serve(ctx context.Context) error {
	if s == nil {
		return nil
	}
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
			_, _ = s.h.Handle(ctx, req.Method, req.Params)
			continue
		}

		res, callErr := s.h.Handle(ctx, req.Method, req.Params)
		if callErr != nil {
			rpcErr := toRPCError(callErr)
			_ = s.w.WriteMessage(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   rpcErr,
			})
			continue
		}

		_ = s.w.WriteMessage(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  res,
		})
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
