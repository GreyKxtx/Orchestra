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

// An RPC already on its way when the core closes — an ops.apply waiting on
// runMu behind a turn — used to find c.tools set to nil by Close and crash
// the process with a nil dereference (test-windows on PR #9). The runner is
// closed, not removed: the late call gets an answer.
func TestClose_ALateRPCDoesNotCrash(t *testing.T) {
	c, _ := setupInitializedCore(t, t.TempDir(), &gateLLM{release: make(chan struct{})})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpsApply(context.Background(), OpsApplyParams{}); err != nil {
		t.Logf("a late ops.apply may fail, but not crash: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
}
