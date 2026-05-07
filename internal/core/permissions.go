package core

import "context"

// PermissionRequester asks the connected client (TUI/IDE) for interactive
// consent before running a sensitive tool (e.g. exec.run).
type PermissionRequester interface {
	RequestPermission(ctx context.Context, req PermissionRequest) (PermissionResponse, error)
}

// PermissionRequest describes the tool action requiring consent.
type PermissionRequest struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	Reason      string `json:"reason,omitempty"`
}

// PermissionResponse is the client's consent decision.
type PermissionResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// rpcPermissionRequester routes PermissionRequest through a server-initiated request function.
type rpcPermissionRequester struct {
	requestFn func(ctx context.Context, method string, params any, result any) error
}

func (r *rpcPermissionRequester) RequestPermission(ctx context.Context, req PermissionRequest) (PermissionResponse, error) {
	var resp PermissionResponse
	if r.requestFn == nil {
		return PermissionResponse{Approved: false}, nil
	}
	if err := r.requestFn(ctx, "permission/request", req, &resp); err != nil {
		return PermissionResponse{Approved: false}, err
	}
	return resp, nil
}
