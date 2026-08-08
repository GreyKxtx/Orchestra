package core

import (
	"context"
	"sync"

	"github.com/orchestra/orchestra/internal/permission"
)

// PermissionRequester / PermissionRequest / PermissionResponse are
// aliases of internal/permission so external callers that referenced
// core.PermissionRequester before H6 (architecture audit) continue to
// compile. New code should depend on internal/permission directly.
type (
	PermissionRequester = permission.Requester
	PermissionRequest   = permission.Request
	PermissionResponse  = permission.Response
)

// rpcPermissionRequester routes a PermissionRequest through the
// server-initiated request function the RPC handler injects.
// Concurrent callers are serialized (FIFO) so shell + lsp.install
// consent never race on the wire / TUI modal.
type rpcPermissionRequester struct {
	requestFn func(ctx context.Context, method string, params any, result any) error
	mu        sync.Mutex
}

func (r *rpcPermissionRequester) RequestPermission(ctx context.Context, req permission.Request) (permission.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var resp permission.Response
	if r.requestFn == nil {
		return permission.Response{Approved: false}, nil
	}
	if err := r.requestFn(ctx, "permission/request", req, &resp); err != nil {
		return permission.Response{Approved: false}, err
	}
	return resp, nil
}
