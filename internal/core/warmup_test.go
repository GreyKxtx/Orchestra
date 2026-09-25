package core

import (
	"context"
	"testing"
	"time"
)

// Close stops the core's warmups before it returns: it cancels them and
// waits. A warmup used to be a bare goroutine that outlived Close — still
// writing to os.Stderr when the web server's test restored it, a data race
// -race caught on CI.
func TestWarmups_StopCancelsAndWaits(t *testing.T) {
	var w warmups
	finished := false // written by the job, read after stop: -race checks the wait
	started := make(chan struct{})
	w.start(context.Background(), func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond) // winding down after the cancel
		finished = true
	})
	<-started
	w.stop()
	if !finished {
		t.Fatal("stop returned before the warmup finished")
	}

	var done warmups
	for i := 0; i < 3; i++ {
		ch := make(chan struct{})
		done.start(context.Background(), func(context.Context) { close(ch) })
		<-ch
	}
	done.stop()
	if len(done.running) != 0 {
		t.Fatalf("finished warmups are forgotten, not kept for the session: %d", len(done.running))
	}

	ran := false
	w.start(context.Background(), func(context.Context) { ran = true })
	w.stop()
	if ran {
		t.Fatal("a warmup started after stop must not run")
	}
}
