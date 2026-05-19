package core

import (
	"context"

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
type rpcPermissionRequester struct {
	requestFn func(ctx context.Context, method string, params any, result any) error
}

func (r *rpcPermissionRequester) RequestPermission(ctx context.Context, req permission.Request) (permission.Response, error) {
	var resp permission.Response
	if r.requestFn == nil {
		return permission.Response{Approved: false}, nil
	}
	if err := r.requestFn(ctx, "permission/request", req, &resp); err != nil {
		return permission.Response{Approved: false}, err
	}
	return resp, nil
}
