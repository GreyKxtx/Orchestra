package jsonrpc

import "testing"

func TestRPCError_Error_IncludesDataDetail(t *testing.T) {
	err := &RPCError{
		Code:    -32603,
		Message: "Internal error",
		Data: map[string]any{
			"error": "context deadline exceeded",
		},
	}
	if got := err.Error(); got != "context deadline exceeded" {
		t.Fatalf("got %q want detail", got)
	}
}

func TestRPCError_Error_AppendsDetail(t *testing.T) {
	err := &RPCError{
		Code:    -32600,
		Message: "Invalid Request",
		Data: map[string]any{
			"error": "session_id is empty",
		},
	}
	if got := err.Error(); got != "Invalid Request: session_id is empty" {
		t.Fatalf("got %q", got)
	}
}
