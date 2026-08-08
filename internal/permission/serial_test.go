package permission

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type orderRequester struct {
	mu    sync.Mutex
	order []string
	gate  chan struct{}
}

func (o *orderRequester) RequestPermission(ctx context.Context, req Request) (Response, error) {
	if req.Tool == "first" {
		o.mu.Lock()
		o.order = append(o.order, "first-enter")
		o.mu.Unlock()
		<-o.gate // hold until second tries to enter SerialRequester
		o.mu.Lock()
		o.order = append(o.order, "first-exit")
		o.mu.Unlock()
		return Response{Approved: true}, nil
	}
	o.mu.Lock()
	o.order = append(o.order, "second")
	o.mu.Unlock()
	return Response{Approved: true}, nil
}

func TestSerialRequester_FIFO(t *testing.T) {
	gate := make(chan struct{})
	inner := &orderRequester{gate: gate}
	serial := WrapSerial(inner)

	var wg sync.WaitGroup
	wg.Add(2)
	var secondStarted atomic.Bool

	go func() {
		defer wg.Done()
		_, _ = serial.RequestPermission(context.Background(), Request{Tool: "first"})
	}()
	time.Sleep(20 * time.Millisecond) // let first acquire lock + enter inner
	go func() {
		defer wg.Done()
		secondStarted.Store(true)
		_, _ = serial.RequestPermission(context.Background(), Request{Tool: "second"})
	}()
	time.Sleep(20 * time.Millisecond)
	if !secondStarted.Load() {
		t.Fatal("second call should be waiting on SerialRequester")
	}
	inner.mu.Lock()
	got := append([]string(nil), inner.order...)
	inner.mu.Unlock()
	for _, s := range got {
		if s == "second" {
			t.Fatalf("second entered before first finished: %v", got)
		}
	}
	close(gate)
	wg.Wait()
	inner.mu.Lock()
	defer inner.mu.Unlock()
	want := []string{"first-enter", "first-exit", "second"}
	if len(inner.order) != len(want) {
		t.Fatalf("order=%v want %v", inner.order, want)
	}
	for i := range want {
		if inner.order[i] != want[i] {
			t.Fatalf("order=%v want %v", inner.order, want)
		}
	}
}
