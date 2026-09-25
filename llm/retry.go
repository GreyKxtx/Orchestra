package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Retrying a request, and relaying a stream, the same way for every provider
// (LLM-14).
//
// The OpenAI-compatible client retried 429s on a fixed linear backoff, deaf
// to the Retry-After its server sent; the Anthropic client did not retry at
// all, and had no stall watchdog, so a dead connection held a step until the
// step timeout. And the agent retried every transient error the client had
// already retried: up to nine POSTs for one step.

// maxRetryAfter is the longest Retry-After a client waits out. A server that
// asks for longer is not waited for inside a step: its error goes up, marked
// as already retried, and the caller decides.
const maxRetryAfter = 60 * time.Second

// StatusError is a provider's non-2xx answer: its status, what it said, and
// the Retry-After it asked for (zero when none). Error() keeps the wording
// the clients always used, which extractStatusCode reads.
type StatusError struct {
	Status     int
	RetryAfter time.Duration
	msg        string
}

func (e *StatusError) Error() string { return e.msg }

// newStatusError wraps a non-2xx answer. text is the message as the client
// has always worded it; h carries Retry-After.
func newStatusError(status int, h http.Header, text string) *StatusError {
	return &StatusError{Status: status, RetryAfter: parseRetryAfter(h, time.Now()), msg: text}
}

// parseRetryAfter reads Retry-After as seconds or as an HTTP date. OpenAI
// also sends retry-after-ms, which wins when present.
func parseRetryAfter(h http.Header, now time.Time) time.Duration {
	if h == nil {
		return 0
	}
	if ms := strings.TrimSpace(h.Get("retry-after-ms")); ms != "" {
		if v, err := strconv.ParseFloat(ms, 64); err == nil && v > 0 {
			return time.Duration(v * float64(time.Millisecond))
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// retryDelay is how long to wait before retry number attempt (1-based) of a
// request that failed with err: the server's Retry-After when it sent one,
// the linear backoff otherwise. ok is false when the server asked for longer
// than maxRetryAfter.
func retryDelay(err error, attempt int) (d time.Duration, ok bool) {
	var se *StatusError
	if errors.As(err, &se) && se.RetryAfter > 0 {
		if se.RetryAfter > maxRetryAfter {
			return 0, false
		}
		return se.RetryAfter, true
	}
	return time.Duration(attempt) * llmRetryBackoff, true
}

// waitRetry sleeps d or until ctx is done.
func waitRetry(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retriedError is a transient error the client has already retried as far
// as it would. The agent does not retry it again.
type retriedError struct {
	err      error
	attempts int
}

func (e *retriedError) Error() string { return e.err.Error() }
func (e *retriedError) Unwrap() error { return e.err }

// markRetried records that err outlived the client's own retries.
func markRetried(err error, attempts int) error {
	if err == nil {
		return nil
	}
	var re *retriedError
	if errors.As(err, &re) {
		return err
	}
	return &retriedError{err: err, attempts: attempts}
}

// AlreadyRetried reports whether a client retried err itself before
// returning it. Retrying it again multiplies the requests — and a server
// that sent Retry-After is waited out already, or asked for longer than a
// step should wait.
func AlreadyRetried(err error) bool {
	var re *retriedError
	return errors.As(err, &re)
}

// stallTimeoutFor scales the idle-stream watchdog with the request timeout
// (llm.timeout_s): a fixed 120s was too aggressive for large local models
// behind a tunnel. At least streamStallTimeout, at most 5 minutes.
func stallTimeoutFor(requestTimeout time.Duration) time.Duration {
	stall := streamStallTimeout
	if scaled := requestTimeout / 5; scaled > stall {
		stall = scaled
	}
	if stall > 5*time.Minute {
		stall = 5 * time.Minute
	}
	return stall
}

// relayStream forwards raw to the returned channel, calling each on every
// event before it is sent (to restore tool names, to log). When no event
// arrives for stall it aborts the stream: abort must close the connection,
// which unblocks the parser; raw is drained, and a "stream stalled" error
// goes out (through each too). done runs once the relay ends.
func relayStream(raw <-chan StreamEvent, stall time.Duration, abort, done func(), each func(*StreamEvent)) <-chan StreamEvent {
	out := make(chan StreamEvent, 16)
	go func() {
		defer close(out)
		defer done()
		timer := time.NewTimer(stall)
		defer timer.Stop()
		for {
			select {
			case ev, ok := <-raw:
				if !ok {
					return
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(stall)
				if each != nil {
					each(&ev)
				}
				out <- ev
			case <-timer.C:
				abort()
				for range raw {
				}
				ev := StreamEvent{Kind: StreamEventError, Err: fmt.Errorf(
					"stream stalled: no data from server for %s (connection to the provider or tunnel lost?)", stall)}
				if each != nil {
					each(&ev)
				}
				out <- ev
				return
			}
		}
	}()
	return out
}
