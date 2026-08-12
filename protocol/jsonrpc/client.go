package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Client is a concurrent JSON-RPC 2.0 client using LSP-style framing.
// Multiple calls may be in-flight simultaneously; responses are demuxed by ID.
// Server-initiated notifications and requests are delivered to optional handlers.
type Client struct {
	w   *Writer
	wMu sync.Mutex

	pMu       sync.Mutex
	nextID    int
	pending   map[string]chan clientMsg
	onNotify  func(method string, params json.RawMessage)
	onRequest func(ctx context.Context, method string, params json.RawMessage) (any, error)

	closeOnce sync.Once
	closed    chan struct{}
}

type clientMsg struct {
	result json.RawMessage
	err    error
}

// wireMsg is used to parse both server responses and server notifications.
type wireMsg struct {
	// Response fields
	ID    json.RawMessage `json:"id"`
	Error *Error          `json:"error"`
	// Both response and notification
	Result json.RawMessage `json:"result"`
	// Notification fields
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// NewClient creates a client that reads from in and writes to out.
// A background goroutine begins reading immediately; close in to shut it down.
func NewClient(in io.Reader, out io.Writer) *Client {
	c := &Client{
		w:       NewWriter(out),
		nextID:  1,
		pending: make(map[string]chan clientMsg),
		closed:  make(chan struct{}),
	}
	go c.readLoop(NewReader(in))
	return c
}

// SetNotificationHandler registers a function called for server-initiated notifications.
// The function is called from the read goroutine; it must not block.
func (c *Client) SetNotificationHandler(fn func(method string, params json.RawMessage)) {
	c.pMu.Lock()
	c.onNotify = fn
	c.pMu.Unlock()
}

// SetRequestHandler registers a function called for server-initiated requests.
// The handler is invoked in a new goroutine per request; it may block.
// Returning an error sends a JSON-RPC error response; returning a value sends a success response.
func (c *Client) SetRequestHandler(fn func(ctx context.Context, method string, params json.RawMessage) (any, error)) {
	c.pMu.Lock()
	c.onRequest = fn
	c.pMu.Unlock()
}

func (c *Client) readLoop(r *Reader) {
	defer func() {
		// A panic in the read loop (malformed payload, handler bug) must not
		// crash the host process: for a TUI that would leave the terminal in
		// raw/alt-screen mode ("bricked"). Treat it as a connection loss —
		// the cleanup below drains pending calls and closes the client.
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "jsonrpc: read loop panic: %v\n", rec)
		}

		// Snapshot and clear pending before closing so new Calls see empty map.
		c.pMu.Lock()
		pending := c.pending
		c.pending = make(map[string]chan clientMsg)
		c.pMu.Unlock()

		// Drain all waiting Calls with a connection-closed error.
		for _, ch := range pending {
			select {
			case ch <- clientMsg{err: fmt.Errorf("jsonrpc: connection closed")}:
			default:
			}
		}

		c.closeOnce.Do(func() { close(c.closed) })
	}()

	for {
		raw, err := r.ReadMessage()
		if err != nil {
			return
		}
		var msg wireMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		// Server-initiated request: has method AND non-null id.
		if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
			c.pMu.Lock()
			fn := c.onRequest
			c.pMu.Unlock()
			if fn == nil {
				// No handler — return method-not-found error.
				c.wMu.Lock()
				_ = c.w.WriteMessage(Response{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Error:   &Error{Code: -32601, Message: "Method not found"},
				})
				c.wMu.Unlock()
				continue
			}
			go func(id json.RawMessage, method string, params json.RawMessage) {
				// A panicking request handler must not kill the process;
				// answer with an internal error instead so the server-side
				// caller unwinds.
				defer func() {
					if rec := recover(); rec != nil {
						c.wMu.Lock()
						_ = c.w.WriteMessage(Response{
							JSONRPC: "2.0",
							ID:      id,
							Error:   &Error{Code: -32603, Message: fmt.Sprintf("client handler panic: %v", rec)},
						})
						c.wMu.Unlock()
					}
				}()
				ctx := context.Background()
				result, err := fn(ctx, method, params)
				c.wMu.Lock()
				defer c.wMu.Unlock()
				if err != nil {
					_ = c.w.WriteMessage(Response{
						JSONRPC: "2.0",
						ID:      id,
						Error:   &Error{Code: -32603, Message: err.Error()},
					})
					return
				}
				var resultRaw json.RawMessage
				if result != nil {
					b, _ := json.Marshal(result)
					resultRaw = b
				}
				_ = c.w.WriteMessage(Response{
					JSONRPC: "2.0",
					ID:      id,
					Result:  resultRaw,
				})
			}(msg.ID, msg.Method, msg.Params)
			continue
		}

		// Notification: has method and absent/null id.
		if msg.Method != "" && (len(msg.ID) == 0 || string(msg.ID) == "null") {
			c.pMu.Lock()
			fn := c.onNotify
			c.pMu.Unlock()
			if fn != nil {
				fn(msg.Method, msg.Params)
			}
			continue
		}

		// Response: has a non-null id.
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			continue
		}
		id := string(msg.ID)
		c.pMu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.pMu.Unlock()
		if !ok {
			// Stale response (e.g. after ctx cancel) — discard.
			continue
		}
		if msg.Error != nil {
			ch <- clientMsg{err: &RPCError{
				Code:    msg.Error.Code,
				Message: msg.Error.Message,
				Data:    msg.Error.Data,
			}}
		} else {
			ch <- clientMsg{result: msg.Result}
		}
	}
}

// Call sends a JSON-RPC request and waits for the matching response.
// Multiple calls may be in-flight concurrently.
// Returns ctx.Err() if ctx is cancelled before the response arrives.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if c == nil {
		return fmt.Errorf("jsonrpc: client is nil")
	}

	c.pMu.Lock()
	id := c.nextID
	c.nextID++
	idStr := fmt.Sprintf("%d", id)
	ch := make(chan clientMsg, 1)
	c.pending[idStr] = ch
	c.pMu.Unlock()

	removePending := func() {
		c.pMu.Lock()
		delete(c.pending, idStr)
		c.pMu.Unlock()
	}

	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			removePending()
			return err
		}
		paramsRaw = b
	}

	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(idStr),
		Method:  method,
		Params:  paramsRaw,
	}
	c.wMu.Lock()
	writeErr := c.w.WriteMessage(req)
	c.wMu.Unlock()
	if writeErr != nil {
		removePending()
		return writeErr
	}

	select {
	case <-ctx.Done():
		removePending()
		// Best-effort cancel notification — server may be mid-call on this id.
		// Fire-and-forget: if the write fails (broken pipe, closed conn), we
		// still return ctx.Err() so the caller unwinds. The server's handler
		// for $/cancelRequest is a notification (no response expected).
		_ = c.notifyCancel(idStr)
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if result != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, result)
		}
		return nil
	case <-c.closed:
		return fmt.Errorf("jsonrpc: connection closed")
	}
}

// notifyCancel sends a JSON-RPC `$/cancelRequest` notification for the given
// in-flight request id. The server uses it to cancel the per-request ctx of
// the matching dispatched call. Returns the write error so the caller can
// log if it likes; cancellation is best-effort and never blocks unwinding.
func (c *Client) notifyCancel(id string) error {
	if c == nil {
		return nil
	}
	params, _ := json.Marshal(map[string]any{"id": id})
	req := Request{
		JSONRPC: "2.0",
		Method:  "$/cancelRequest",
		Params:  params,
	}
	c.wMu.Lock()
	defer c.wMu.Unlock()
	return c.w.WriteMessage(req)
}
