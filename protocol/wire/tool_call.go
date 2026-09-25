package wire

import (
	"encoding/json"
)

// tool.call.

type ToolCallParams struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
