package llm

import "strings"

// toolCallState tracks one tool call being assembled across stream chunks.
type toolCallState struct {
	id   string
	name string
	args strings.Builder
	seen bool // true after ToolCallStart event was emitted
}

// toolCallAccumulator assembles partial tool_call chunks from an SSE stream.
//
// OpenAI sends id+name in the first chunk and arguments in later chunks.
// vLLM may send the complete call (name + full arguments) in a single chunk.
// The chunk's "index" field is the stable key; id may be absent on later chunks.
type toolCallAccumulator struct {
	content strings.Builder
	calls   []*toolCallState  // ordered by first-seen index
	byIndex map[int]*toolCallState
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{
		byIndex: make(map[int]*toolCallState, 4),
	}
}

// AppendContent records a text delta from the model.
func (a *toolCallAccumulator) AppendContent(s string) {
	a.content.WriteString(s)
}

// FeedToolCall ingests one tool_calls entry from a stream chunk.
// Returns (isNew, resolvedName, storedID):
//   - isNew:        true the first time this call's name is known
//   - resolvedName: the tool name (only meaningful when isNew=true)
//   - storedID:     the stable id (from the first chunk that included it; may be "")
func (a *toolCallAccumulator) FeedToolCall(index int, id, name, args string) (isNew bool, resolvedName, storedID string) {
	tc, ok := a.byIndex[index]
	if !ok {
		tc = &toolCallState{}
		a.byIndex[index] = tc
		a.calls = append(a.calls, tc)
	}
	if id != "" && tc.id == "" {
		tc.id = id
	}
	if name != "" && tc.name == "" {
		tc.name = name
	}
	if args != "" {
		tc.args.WriteString(args)
	}
	if !tc.seen && tc.name != "" {
		tc.seen = true
		return true, tc.name, tc.id
	}
	return false, "", tc.id
}

// BuildResponse assembles the final CompleteResponse from all accumulated state.
func (a *toolCallAccumulator) BuildResponse() *CompleteResponse {
	msg := Message{Role: RoleAssistant}
	if a.content.Len() > 0 {
		msg.Content = a.content.String()
	}
	for _, tc := range a.calls {
		args := tc.args.String()
		if args == "" {
			args = "{}"
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:   tc.id,
			Type: "function",
			Function: ToolCallFunc{
				Name:      tc.name,
				Arguments: ToolArguments([]byte(args)),
			},
		})
	}
	return &CompleteResponse{Message: msg}
}
