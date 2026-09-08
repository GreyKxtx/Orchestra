package webtransport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

// dialTestPipe stands up a real HTTP server that accepts one WebSocket and
// hands its Pipe to the test. Real sockets, not a fake: the framing bug this
// bridge exists to avoid only shows up on a real connection.
func dialTestPipe(t *testing.T) (client *websocket.Conn, server *Pipe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	ready := make(chan *Pipe, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(jsonrpc.DefaultMaxContentBytes)
		p := NewPipe(ctx, c)
		ready <- p
		<-ctx.Done()
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + srv.URL[len("http"):]
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.CloseNow() })

	select {
	case server = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("server never produced a Pipe")
	}
	return client, server
}

func TestPipe_FrameInBecomesFramedRead(t *testing.T) {
	ctx := context.Background()
	client, p := dialTestPipe(t)

	// The browser sends one plain JSON object per frame — no Content-Length.
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); err != nil {
		t.Fatalf("client write: %v", err)
	}

	// The server side must read it through the LSP-framing Reader.
	msg, err := jsonrpc.NewReader(p.Reader()).ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", msg, err)
	}
	if got["method"] != "ping" {
		t.Fatalf("method = %v, want ping", got["method"])
	}
}

func TestPipe_FramedWriteBecomesOneFrame(t *testing.T) {
	ctx := context.Background()
	client, p := dialTestPipe(t)

	// A payload larger than bufio's 4096-byte buffer: WriteMessage issues
	// several underlying Writes for it, and the browser must still see exactly
	// one frame carrying exactly one JSON object.
	big := make([]byte, 10000)
	for i := range big {
		big[i] = 'x'
	}
	if err := jsonrpc.NewWriter(p.Writer()).WriteMessage(map[string]any{
		"jsonrpc": "2.0",
		"method":  "agent/event",
		"params":  map[string]any{"content": string(big)},
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	typ, frame, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want MessageText", typ)
	}
	var got struct {
		Method string `json:"method"`
		Params struct {
			Content string `json:"content"`
		} `json:"params"`
	}
	if err := json.Unmarshal(frame, &got); err != nil {
		t.Fatalf("frame is not one JSON object (%d bytes): %v", len(frame), err)
	}
	if got.Method != "agent/event" {
		t.Fatalf("method = %q, want agent/event", got.Method)
	}
	if len(got.Params.Content) != len(big) {
		t.Fatalf("content len = %d, want %d", len(got.Params.Content), len(big))
	}
}

func TestPipe_ClientCloseEndsReaderWithEOF(t *testing.T) {
	client, p := dialTestPipe(t)

	if err := client.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("client close: %v", err)
	}

	// This is the property the whole design rests on: a closed tab must reach
	// jsonrpc.Server.Serve as EOF, so it returns and pending server-initiated
	// requests die with it.
	done := make(chan error, 1)
	go func() {
		_, err := jsonrpc.NewReader(p.Reader()).ReadMessage()
		done <- err
	}()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("read after close = %v, want io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not unblock after the client closed")
	}
}
