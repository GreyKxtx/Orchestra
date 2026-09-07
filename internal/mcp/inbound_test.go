package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// wiredClient is a Client hooked to in-memory pipes: lines written to serverW
// arrive as if the server sent them, and everything the client writes is
// collected in sent.
type wiredClient struct {
	c       *Client
	serverW io.WriteCloser
	mu      sync.Mutex
	sent    []string
	gotLine chan struct{}
}

func newWiredClient(t *testing.T, onRequest inboundHandler) *wiredClient {
	t.Helper()
	serverR, serverW := io.Pipe() // server → client
	clientR, clientW := io.Pipe() // client → server

	w := &wiredClient{serverW: serverW, gotLine: make(chan struct{}, 32)}
	w.c = &Client{
		name:      "test",
		stdin:     clientW,
		stdout:    bufio.NewScanner(serverR),
		pending:   map[int64]chan rpcResponse{},
		done:      make(chan struct{}),
		onRequest: onRequest,
	}
	go w.c.readLoop()
	go func() {
		sc := bufio.NewScanner(clientR)
		for sc.Scan() {
			w.mu.Lock()
			w.sent = append(w.sent, sc.Text())
			w.mu.Unlock()
			select {
			case w.gotLine <- struct{}{}:
			default:
			}
		}
	}()
	t.Cleanup(func() {
		_ = serverW.Close()
		_ = clientW.Close()
	})
	return w
}

// serverSends writes one JSON line as if it came from the MCP server.
func (w *wiredClient) serverSends(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(w.serverW, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

// waitForLine blocks until the client writes something, or fails.
func (w *wiredClient) waitForLine(t *testing.T) string {
	t.Helper()
	select {
	case <-w.gotLine:
	case <-time.After(3 * time.Second):
		t.Fatal("client wrote nothing; a server request that gets no reply hangs the server forever")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sent[len(w.sent)-1]
}

// Sampling and elicitation are server→client REQUESTS. The read loop treated
// every message carrying an id as a response to something we sent, looked it
// up in the pending map, found nothing and dropped it — leaving the server
// waiting for a reply that was never coming.
func TestReadLoop_AnswersAnInboundRequest(t *testing.T) {
	w := newWiredClient(t, func(_ context.Context, method string, params json.RawMessage) (any, *rpcError) {
		if method != "sampling/createMessage" {
			return nil, &rpcError{Code: -32601, Message: "method not found"}
		}
		return map[string]any{"model": "test-model", "echo": string(params)}, nil
	})

	w.serverSends(t, `{"jsonrpc":"2.0","id":7,"method":"sampling/createMessage","params":{"maxTokens":10}}`)

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int64          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *rpcError       `json:"error"`
	}
	if err := json.Unmarshal([]byte(w.waitForLine(t)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == nil || *resp.ID != 7 {
		t.Fatalf("reply id = %v, want 7 — a reply the server cannot match is as bad as none", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("reply carried an error: %+v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), "test-model") {
		t.Errorf("result = %s, want the handler's answer", resp.Result)
	}
}

// A method we do not implement must be refused explicitly. Silence is the one
// answer that hangs the caller.
func TestReadLoop_RefusesAnUnknownInboundMethod(t *testing.T) {
	w := newWiredClient(t, nil) // no handler wired at all

	w.serverSends(t, `{"jsonrpc":"2.0","id":3,"method":"roots/list"}`)

	var resp struct {
		ID    *int64    `json:"id"`
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(w.waitForLine(t)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == nil || *resp.ID != 3 {
		t.Fatalf("reply id = %v, want 3", resp.ID)
	}
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("want a JSON-RPC method-not-found error, got %+v", resp.Error)
	}
}

// A slow handler must not stall the read loop: responses to our own calls
// arrive on the same pipe, so blocking there deadlocks the client against a
// server that is waiting for us to answer.
func TestReadLoop_SlowInboundRequestDoesNotBlockResponses(t *testing.T) {
	release := make(chan struct{})
	w := newWiredClient(t, func(ctx context.Context, _ string, _ json.RawMessage) (any, *rpcError) {
		<-release
		return map[string]any{"ok": true}, nil
	})

	// Register a pending call, as Client.call would.
	ch := make(chan rpcResponse, 1)
	w.c.mu.Lock()
	w.c.pending[42] = ch
	w.c.mu.Unlock()

	w.serverSends(t, `{"jsonrpc":"2.0","id":1,"method":"elicitation/create"}`)
	w.serverSends(t, `{"jsonrpc":"2.0","id":42,"result":{"done":true}}`)

	select {
	case got := <-ch:
		if !strings.Contains(string(got.Result), "done") {
			t.Errorf("dispatched the wrong response: %s", got.Result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a response was not dispatched while an inbound request was in flight — " +
			"the read loop is blocked on the handler")
	}
	close(release)
}

// Notifications (no id) must keep working exactly as before.
func TestReadLoop_StillIgnoresNotifications(t *testing.T) {
	w := newWiredClient(t, func(context.Context, string, json.RawMessage) (any, *rpcError) {
		t.Error("a notification was dispatched as a request")
		return nil, nil
	})
	w.serverSends(t, `{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`)

	select {
	case <-w.gotLine:
		t.Fatal("the client replied to a notification; notifications take no reply")
	case <-time.After(300 * time.Millisecond):
	}
}

// The per-request token cap bounds one sampling call. Nothing bounded how many
// a server could have in flight at once — so a server could sidestep the cap
// by sending a thousand requests, each its own goroutine, each its own LLM
// call, each its own consent prompt. A cap that is trivially multiplied is not
// a cap.
func TestReadLoop_BoundsConcurrentInboundRequests(t *testing.T) {
	entered := make(chan struct{}, 64)
	release := make(chan struct{})
	w := newWiredClient(t, func(context.Context, string, json.RawMessage) (any, *rpcError) {
		entered <- struct{}{}
		<-release
		return map[string]any{"ok": true}, nil
	})
	defer close(release)

	const flood = 32
	for i := 1; i <= flood; i++ {
		w.serverSends(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"sampling/createMessage"}`, i))
	}

	// Everything over the limit must be refused promptly rather than queued:
	// a queue lets the server pile up work and starve later requests.
	deadline := time.After(3 * time.Second)
	refused := 0
	for refused == 0 {
		select {
		case <-w.gotLine:
			w.mu.Lock()
			last := w.sent[len(w.sent)-1]
			w.mu.Unlock()
			if strings.Contains(last, "error") {
				refused++
			}
		case <-deadline:
			t.Fatalf("no request was refused after flooding %d of them; "+
				"in-flight handlers are unbounded", flood)
		}
	}

	inFlight := len(entered)
	if inFlight > maxInFlightInbound {
		t.Errorf("%d handlers ran concurrently, limit is %d", inFlight, maxInFlightInbound)
	}
}
