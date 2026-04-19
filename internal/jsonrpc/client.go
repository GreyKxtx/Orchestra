package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Client is a concurrent JSON-RPC 2.0 client using LSP-style framing.
// Multiple calls may be in-flight simultaneously; responses are demuxed by ID.
// Server-initiated notifications are delivered to an optional handler.
type Client struct {
	w   *Writer
	wMu sync.Mutex

	pMu      sync.Mutex
	nextID   int
	pending  map[string]chan clientMsg
	onNotify func(method string, params json.RawMessage)

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

func (c *Client) readLoop(r *Reader) {
	defer func() {
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
