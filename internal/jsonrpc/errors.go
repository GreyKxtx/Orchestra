package jsonrpc

import "fmt"

// RPCError is an error that carries a JSON-RPC error object payload.
// It is used for standard JSON-RPC errors like -32601 (Method not found).
type RPCError struct {
	Code    int
	Message string
	Data    any
}

func (e *RPCError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("jsonrpc error %d", e.Code)
}

func MethodNotFound(method string) *RPCError {
	return &RPCError{
		Code:    -32601,
		Message: "Method not found",
		Data:    map[string]any{"method": method},
	}
}
