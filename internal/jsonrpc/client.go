package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Client is a minimal synchronous JSON-RPC 2.0 client using LSP-style framing.
//
// It supports one in-flight request at a time (serialized with a mutex),
// which is sufficient for the CLI use-case.
type Client struct {
	r *Reader
	w *Writer

	mu     sync.Mutex
	nextID int
}

func NewClient(in io.Reader, out io.Writer) *Client {
	return &Client{
		r:      NewReader(in),
		w:      NewWriter(out),
		nextID: 1,
	}
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if c == nil {
		return fmt.Errorf("client is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		paramsRaw = b
	}

	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  paramsRaw,
	}
	if err := c.w.WriteMessage(req); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := c.r.ReadMessage()
		if err != nil {
			return err
		}
		var resp Response
		if err := json.Unmarshal(msg, &resp); err != nil {
			return err
		}
		// Match id (best-effort).
		if string(resp.ID) != fmt.Sprintf("%d", id) && string(resp.ID) != fmt.Sprintf("\"%d\"", id) {
			// Not our response (shouldn't happen with serialized calls); ignore.
			continue
		}

		if resp.Error != nil {
			return &RPCError{
				Code:    resp.Error.Code,
				Message: resp.Error.Message,
				Data:    resp.Error.Data,
			}
		}

		if result != nil {
			b, err := json.Marshal(resp.Result)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(b, result); err != nil {
				return err
			}
		}
		return nil
	}
}
