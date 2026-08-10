package core

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

const defaultStreamDebounceMS = 30

// streamDebounceInterval returns the coalesce window for message_delta /
// reasoning_delta RPC notifications. ORCH_STREAM_DEBOUNCE_MS=0 disables debounce.
func streamDebounceInterval() time.Duration {
	raw := os.Getenv("ORCH_STREAM_DEBOUNCE_MS")
	if raw == "" {
		return time.Duration(defaultStreamDebounceMS) * time.Millisecond
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

type deltaDebouncer struct {
	interval time.Duration
	emit     func(agent.AgentEvent)

	mu      sync.Mutex
	timer   *time.Timer
	pending *agent.AgentEvent
}

func newDeltaDebouncer(interval time.Duration, emit func(agent.AgentEvent)) *deltaDebouncer {
	return &deltaDebouncer{interval: interval, emit: emit}
}

func (d *deltaDebouncer) handle(ev agent.AgentEvent) {
	kind := ev.Stream.Kind
	if kind != llm.StreamEventMessageDelta && kind != llm.StreamEventReasoningDelta {
		d.flush()
		d.emit(ev)
		return
	}

	var preempt *agent.AgentEvent
	d.mu.Lock()
	if d.pending != nil && (d.pending.Step != ev.Step || d.pending.Stream.Kind != kind) {
		preempt = d.takePendingLocked()
	}
	if d.pending == nil {
		copy := ev
		d.pending = &copy
	} else {
		d.pending.Stream.Content += ev.Stream.Content
	}
	d.scheduleFlushLocked()
	d.mu.Unlock()

	if preempt != nil {
		d.emit(*preempt)
	}
}

func (d *deltaDebouncer) flush() {
	var ev *agent.AgentEvent
	d.mu.Lock()
	ev = d.takePendingLocked()
	d.mu.Unlock()
	if ev != nil {
		d.emit(*ev)
	}
}

func (d *deltaDebouncer) takePendingLocked() *agent.AgentEvent {
	if d.pending == nil {
		return nil
	}
	ev := *d.pending
	d.pending = nil
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	return &ev
}

func (d *deltaDebouncer) scheduleFlushLocked() {
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.interval, d.flush)
}

func wrapStreamDebounce(emit func(agent.AgentEvent)) func(agent.AgentEvent) {
	if iv := streamDebounceInterval(); iv > 0 {
		return newDeltaDebouncer(iv, emit).handle
	}
	return emit
}
