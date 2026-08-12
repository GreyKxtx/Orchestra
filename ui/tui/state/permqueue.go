package state

// PermRequest is one server-initiated permission request (permission/request
// RPC) waiting for a user decision.
type PermRequest struct {
	ReqID       int64
	Tool        string
	Description string
	Kind        string
}

// PermQueue is the state machine behind the permission modal: at most one
// request is current (presented to the user), the rest wait in FIFO order.
// Transitions are pure — ordering and correlation are unit-testable without
// the UI or a live core.
type PermQueue struct {
	current *PermRequest
	waiting []PermRequest
}

// Push registers a new request. Returns true when the request became current
// and must be presented; false when it was queued behind the current one.
func (q *PermQueue) Push(r PermRequest) bool {
	if q.current != nil {
		q.waiting = append(q.waiting, r)
		return false
	}
	q.current = &r
	return true
}

// Current returns the request being presented, or ok=false when idle.
func (q *PermQueue) Current() (PermRequest, bool) {
	if q.current == nil {
		return PermRequest{}, false
	}
	return *q.current, true
}

// Answer consumes the current request (a decision was made) and returns it
// so the answer can be correlated by ReqID.
func (q *PermQueue) Answer() (PermRequest, bool) {
	if q.current == nil {
		return PermRequest{}, false
	}
	r := *q.current
	q.current = nil
	return r, true
}

// Promote pops the next waiting request into current. ok=false when a
// request is already presented or nothing is waiting.
func (q *PermQueue) Promote() (PermRequest, bool) {
	if q.current != nil || len(q.waiting) == 0 {
		return PermRequest{}, false
	}
	r := q.waiting[0]
	q.waiting = q.waiting[1:]
	q.current = &r
	return r, true
}

// Reset drops the current request and the whole FIFO — used on core respawn
// when every outstanding request is void.
func (q *PermQueue) Reset() {
	q.current = nil
	q.waiting = nil
}

// Waiting returns how many requests sit behind the current one.
func (q *PermQueue) Waiting() int { return len(q.waiting) }
