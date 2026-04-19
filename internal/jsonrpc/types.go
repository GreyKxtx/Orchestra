package jsonrpc

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Request is a JSON-RPC 2.0 request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type wireRequest struct {
	JSONRPC json.RawMessage `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  json.RawMessage `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type parsedRequest struct {
	ID             json.RawMessage
	Method         string
	Params         json.RawMessage
	IsNotification bool
}

type payloadError struct {
	Code    int
	Message string
	Data    any
}

func parsePayload(payload []byte) (parsedRequest, *payloadError) {
	p := bytes.TrimSpace(payload)
	if len(p) == 0 {
		return parsedRequest{}, &payloadError{
			Code:    -32700,
			Message: "Parse error",
			Data:    map[string]any{"error": "empty payload"},
		}
	}
	if !json.Valid(p) {
		var v any
		err := json.Unmarshal(p, &v)
		msg := "invalid json"
		if err != nil {
			msg = err.Error()
		}
		return parsedRequest{}, &payloadError{
			Code:    -32700,
			Message: "Parse error",
			Data:    map[string]any{"error": msg},
		}
	}

	switch p[0] {
	case '{':
		var wr wireRequest
		if err := json.Unmarshal(p, &wr); err != nil {
			return parsedRequest{}, &payloadError{
				Code:    -32700,
				Message: "Parse error",
				Data:    map[string]any{"error": err.Error()},
			}
		}

		var ver string
		if len(wr.JSONRPC) == 0 || json.Unmarshal(wr.JSONRPC, &ver) != nil || ver != "2.0" {
			return parsedRequest{}, &payloadError{Code: -32600, Message: "Invalid Request"}
		}

		var method string
		if len(wr.Method) == 0 || json.Unmarshal(wr.Method, &method) != nil || strings.TrimSpace(method) == "" {
			return parsedRequest{}, &payloadError{Code: -32600, Message: "Invalid Request"}
		}

		isNotif, idValid := classifyID(wr.ID)
		if !idValid {
			return parsedRequest{}, &payloadError{Code: -32600, Message: "Invalid Request"}
		}

		return parsedRequest{
			ID:             wr.ID,
			Method:         method,
			Params:         wr.Params,
			IsNotification: isNotif,
		}, nil

	case '[':
		// Batch requests are valid JSON, but not supported.
		return parsedRequest{}, &payloadError{Code: -32600, Message: "Invalid Request"}

	default:
		// Valid JSON, but not an object/array.
		return parsedRequest{}, &payloadError{Code: -32600, Message: "Invalid Request"}
	}
}
