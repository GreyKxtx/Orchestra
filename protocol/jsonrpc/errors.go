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
	detail := rpcErrorDetail(e.Data)
	switch {
	case detail != "" && e.Message != "" && detail != e.Message:
		if e.Message == "Internal error" || e.Code == -32603 {
			return detail
		}
		return e.Message + ": " + detail
	case detail != "":
		return detail
	case e.Message != "":
		return e.Message
	default:
		return fmt.Sprintf("jsonrpc error %d", e.Code)
	}
}

func rpcErrorDetail(data any) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	if s, ok := m["error"].(string); ok {
		return s
	}
	return ""
}

func MethodNotFound(method string) *RPCError {
	return &RPCError{
		Code:    -32601,
		Message: "Method not found",
		Data:    map[string]any{"method": method},
	}
}
