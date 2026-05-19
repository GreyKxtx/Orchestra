package subproc

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestStderrRing_Append(t *testing.T) {
	r := NewStderrRing(64)
	r.Drain(strings.NewReader("hello"))
	if got := r.Tail(); got != "hello" {
		t.Fatalf("Tail=%q, want %q", got, "hello")
	}
}

func TestStderrRing_BoundedAtCap(t *testing.T) {
	r := NewStderrRing(8)
	r.Drain(strings.NewReader("0123456789ABCDEF")) // 16 bytes
	got := r.Tail()
	if len(got) != 8 {
		t.Fatalf("len(Tail)=%d, want 8", len(got))
	}
	if got != "89ABCDEF" {
		t.Fatalf("Tail=%q, want last 8 bytes", got)
	}
}

func TestStderrRing_ZeroSizeDefault(t *testing.T) {
	r := NewStderrRing(0)
	// Synthetic 80 KiB stream — drain should keep at most DefaultRingSize.
	stream := bytes.Repeat([]byte("x"), 80*1024)
	r.Drain(bytes.NewReader(stream))
	if got := len(r.Tail()); got > DefaultRingSize {
		t.Fatalf("len(Tail)=%d, want <=%d", got, DefaultRingSize)
	}
}

func TestStderrRing_NilSafe(t *testing.T) {
	var r *StderrRing
	r.Drain(strings.NewReader("ignored"))
	if r.Tail() != "" {
		t.Fatalf("nil Tail must be empty")
	}
}

func TestStderrRing_ConcurrentDrainTail(t *testing.T) {
	r := NewStderrRing(1024)
	stream := bytes.Repeat([]byte("y"), 4096)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.Drain(bytes.NewReader(stream)) }()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = r.Tail()
		}
	}()
	wg.Wait()
}
