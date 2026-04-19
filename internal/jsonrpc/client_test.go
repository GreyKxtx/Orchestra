package jsonrpc

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
)

// serverPipe wires a Server and Client together via in-process pipes.
func serverPipe(t *testing.T, h Handler) (*Server, *Client) {
	t.Helper()
	// Client writes to serverIn; Server reads from serverIn.
	// Server writes to clientIn; Client reads from clientIn.
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()

	srv := NewServer(h, serverIn, serverOut)
	cli := NewClient(clientIn, clientOut)

	t.Cleanup(func() {
		_ = clientOut.Close()
		_ = clientIn.Close()
		_ = serverIn.Close()
		_ = serverOut.Close()
	})
	return srv, cli
}

func TestClient_BasicCall(t *testing.T) {
	srv, cli := serverPipe(t, testHandler{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	var res map[string]any
	if err := cli.Call(ctx, "core.health", nil, &res); err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("unexpected result: %v", res)
	}
}

func TestClient_ContextCancel(t *testing.T) {
	// A handler that blocks until its context is done.
	blocked := make(chan struct{})
	h := blockingHandler{blocked: blocked}
	srv, cli := serverPipe(t, h)

	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()
	go func() { _ = srv.Serve(srvCtx) }()

	callCtx, callCancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- cli.Call(callCtx, "block", nil, nil)
	}()

	callCancel()

	if err := <-errCh; err == nil {
		t.Fatal("expected error after cancel, got nil")
	}
	close(blocked) // unblock handler so server can clean up
}

func TestClient_Notification(t *testing.T) {
	srv, cli := serverPipe(t, testHandler{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	var mu sync.Mutex
	var received []string

	cli.SetNotificationHandler(func(method string, params json.RawMessage) {
		mu.Lock()
		received = append(received, method)
		mu.Unlock()
	})

	// Trigger a notification by calling a method that emits one.
	notifySrv := srv // server sends notification via Notify
	go func() { _ = notifySrv.Notify("test/event", map[string]any{"x": 1}) }()

	// Give the notification time to arrive.
	var health map[string]any
	_ = cli.Call(ctx, "core.health", nil, &health) // synchronize

	mu.Lock()
	got := received
	mu.Unlock()
	if len(got) != 1 || got[0] != "test/event" {
		t.Fatalf("expected [test/event], got %v", got)
	}
}

func TestClient_ConcurrentCalls(t *testing.T) {
	srv, cli := serverPipe(t, testHandler{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res map[string]any
			errs[i] = cli.Call(ctx, "core.health", nil, &res)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
}

// blockingHandler blocks until the blocked channel is closed.
type blockingHandler struct {
	blocked chan struct{}
}

func (h blockingHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == "block" {
		select {
		case <-h.blocked:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	}
	return nil, MethodNotFound(method)
}
