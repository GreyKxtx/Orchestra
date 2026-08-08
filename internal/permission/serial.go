package permission

import (
	"context"
	"sync"
)

// SerialRequester FIFO-serializes RequestPermission so only one consent
// modal is in flight at a time (shell + lsp.install cannot race).
type SerialRequester struct {
	Inner Requester
	mu    sync.Mutex
}

// RequestPermission implements Requester.
func (s *SerialRequester) RequestPermission(ctx context.Context, req Request) (Response, error) {
	if s == nil || s.Inner == nil {
		return Response{Approved: false}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Inner.RequestPermission(ctx, req)
}

// WrapSerial returns a FIFO serializer around inner (nil-safe).
func WrapSerial(inner Requester) Requester {
	if inner == nil {
		return nil
	}
	if _, ok := inner.(*SerialRequester); ok {
		return inner
	}
	return &SerialRequester{Inner: inner}
}
