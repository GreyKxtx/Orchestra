package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type testHandler struct{}

func (h testHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	_ = ctx
	switch method {
	case "core.health":
		return map[string]any{"status": "ok"}, nil
	default:
		return nil, MethodNotFound(method)
	}
}

func TestServer_StdioFraming_Health(t *testing.T) {
	in := framed(`{"jsonrpc":"2.0","id":1,"method":"core.health","params":{}}`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if string(resp.ID) != "1" {
		t.Fatalf("expected id=1, got %s", string(resp.ID))
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	if m["status"] != "ok" {
		t.Fatalf("unexpected status: %v", m["status"])
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	in := framed(`{"jsonrpc":"2.0","id":"x","method":"nope","params":{}}`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if resp.Error == nil {
		t.Fatalf("expected error, got nil")
	}
	if string(resp.ID) != `"x"` {
		t.Fatalf("expected id=\"x\", got %s", string(resp.ID))
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected -32601, got %d", resp.Error.Code)
	}
}

func TestWriter_ConcurrentWrites_NoInterleaving(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			if err := w.WriteMessage(map[string]int{"i": i}); err != nil {
				t.Errorf("WriteMessage failed: %v", err)
			}
		}()
	}
	wg.Wait()

	rd := NewReader(bytes.NewReader(buf.Bytes()))
	seen := make(map[int]bool, n)
	for {
		msg, err := rd.ReadMessage()
		if err != nil {
			break
		}
		var m map[string]int
		if err := json.Unmarshal(msg, &m); err != nil {
			t.Fatalf("invalid json payload: %v", err)
		}
		seen[m["i"]] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d messages, got %d", n, len(seen))
	}
}

func TestServer_MessageTooLarge_ReturnsParseError(t *testing.T) {
	// Default limit is 4MB; send a larger Content-Length without a body.
	in := "Content-Length: 5000000\r\n\r\n"
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if resp.Error == nil {
		t.Fatalf("expected error, got nil")
	}
	if resp.Error.Code != -32700 {
		t.Fatalf("expected parse error -32700, got %d", resp.Error.Code)
	}
}

func TestServer_Notification_NoResponse(t *testing.T) {
	in := framed(`{"jsonrpc":"2.0","method":"core.health","params":{}}`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("expected no response for notification, got: %q", out.String())
	}
}

func TestServer_MultipleMessages_OneResponsePerRequest(t *testing.T) {
	in := framed(`{"jsonrpc":"2.0","id":1,"method":"core.health","params":{}}`) +
		framed(`{"jsonrpc":"2.0","method":"core.health","params":{}}`) + // notification
		framed(`{"jsonrpc":"2.0","id":2,"method":"nope","params":{}}`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	rd := NewReader(strings.NewReader(out.String()))
	var ids []string
	var codes []int
	for {
		msg, err := rd.ReadMessage()
		if err != nil {
			break
		}
		var resp Response
		if err := json.Unmarshal(msg, &resp); err != nil {
			t.Fatalf("unmarshal response failed: %v\npayload=%s", err, string(msg))
		}
		ids = append(ids, string(resp.ID))
		if resp.Error != nil {
			codes = append(codes, resp.Error.Code)
		} else {
			codes = append(codes, 0)
		}
	}

	if len(ids) != 2 {
		t.Fatalf("expected 2 responses (2 requests, 1 notification), got %d: %v", len(ids), ids)
	}
	if ids[0] != "1" || codes[0] != 0 {
		t.Fatalf("unexpected first response: id=%s code=%d", ids[0], codes[0])
	}
	if ids[1] != "2" || codes[1] != -32601 {
		t.Fatalf("unexpected second response: id=%s code=%d", ids[1], codes[1])
	}
}

func TestServer_IDNull_IsRequest_ResponseHasIDNull(t *testing.T) {
	in := framed(`{"jsonrpc":"2.0","id":null,"method":"core.health","params":{}}`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("expected id=null, got %s", string(resp.ID))
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	if m["status"] != "ok" {
		t.Fatalf("unexpected status: %v", m["status"])
	}
}

func TestServer_InvalidID_InvalidRequest_IDNull(t *testing.T) {
	in := framed(`{"jsonrpc":"2.0","id":{},"method":"core.health","params":{}}`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("expected id=null, got %s", string(resp.ID))
	}
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Fatalf("expected Invalid Request -32600, got %+v", resp.Error)
	}
}

func TestServer_InvalidMethodType_InvalidRequest_IDNull(t *testing.T) {
	in := framed(`{"jsonrpc":"2.0","id":1,"method":1,"params":{}}`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("expected id=null, got %s", string(resp.ID))
	}
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Fatalf("expected Invalid Request -32600, got %+v", resp.Error)
	}
}

func TestServer_BatchRequest_Unsupported_InvalidRequest_IDNull(t *testing.T) {
	in := framed(`[{"jsonrpc":"2.0","id":1,"method":"core.health","params":{}}]`)
	var out strings.Builder

	s := NewServer(testHandler{}, strings.NewReader(in), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("expected id=null, got %s", string(resp.ID))
	}
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Fatalf("expected Invalid Request -32600, got %+v", resp.Error)
	}
}

func TestReader_ContentLength_CRLFAndWhitespace(t *testing.T) {
	rd := NewReader(strings.NewReader("Content-Length : 5\r\n\r\nhello"))
	msg, err := rd.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if string(msg) != "hello" {
		t.Fatalf("expected hello, got %q", string(msg))
	}
}

func TestReader_ContentLength_LFOnly_OK(t *testing.T) {
	rd := NewReader(strings.NewReader("Content-Length: 5\n\nhello"))
	msg, err := rd.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if string(msg) != "hello" {
		t.Fatalf("expected hello, got %q", string(msg))
	}
}

func TestReader_ContentLength_InvalidValue_Rejected(t *testing.T) {
	rd := NewReader(strings.NewReader("Content-Length:  00012abc\r\n\r\n"))
	_, err := rd.ReadMessage()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestReader_ContentLength_Duplicate_Rejected_CaseInsensitive(t *testing.T) {
	rd := NewReader(strings.NewReader("Content-Length: 1\r\ncontent-length: 1\r\n\r\nx"))
	_, err := rd.ReadMessage()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestServer_PartialReads_OK(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"method":"core.health","params":{}}`
	in := []byte(framed(payload))
	var out strings.Builder

	s := NewServer(testHandler{}, &chunkReader{b: in, chunk: 3}, &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	respJSON := firstPayload(out.String())
	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v\npayload=%s", err, respJSON)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func framed(payload string) string {
	return "Content-Length: " + itoa(len(payload)) + "\r\n\r\n" + payload
}

func firstPayload(framedOut string) string {
	// Find header terminator, assume single message.
	i := strings.Index(framedOut, "\r\n\r\n")
	if i < 0 {
		return ""
	}
	return framedOut[i+4:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return sign + string(b[i:])
}

type chunkReader struct {
	b     []byte
	chunk int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r == nil {
		return 0, io.EOF
	}
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if r.chunk > 0 && n > r.chunk {
		n = r.chunk
	}
	if n > len(r.b) {
		n = len(r.b)
	}
	copy(p[:n], r.b[:n])
	r.b = r.b[n:]
	return n, nil
}

// TestServer_RequestRoundTrip verifies that Server.Request sends a server-initiated
// JSON-RPC request to the client and correctly receives the client's response.
func TestServer_RequestRoundTrip(t *testing.T) {
	// Wire up bidirectional pipes: client-to-server and server-to-client.
	// io.Pipe() returns (*PipeReader, *PipeWriter).
	sFromC, cToS := io.Pipe() // server reads sFromC; client writes cToS
	cFromS, sToC := io.Pipe() // client reads cFromS; server writes sToC

	// Server reads from sFromC and writes to sToC.
	srv := NewServer(testHandler{}, sFromC, sToC)
	// Client reads from cFromS and writes to cToS.
	cli := NewClient(cFromS, cToS)

	// Client handles the server-initiated permission/request.
	cli.SetRequestHandler(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != "permission/request" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return map[string]any{"approved": true}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Serve in background; stops when pipes are closed or context is done.
	go func() { _ = srv.Serve(ctx) }()

	var result map[string]any
	err := srv.Request(ctx, "permission/request", map[string]any{"tool": "exec.run"}, &result)
	if err != nil {
		t.Fatalf("Server.Request: %v", err)
	}
	approved, ok := result["approved"].(bool)
	if !ok || !approved {
		t.Fatalf("expected approved=true, got %+v", result)
	}

	// Close the server's read pipe to stop the serve loop cleanly.
	_ = cToS.Close()  // signals EOF to server's reader (sFromC)
	_ = sToC.Close()  // clean up server's write end
}
