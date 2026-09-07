package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// JSON-RPC error codes used for server→client requests we cannot serve.
const (
	rpcMethodNotFound = -32601
	rpcInternalError  = -32603
)

// inboundRequestTimeout bounds one server→client request. Sampling runs an LLM
// call and elicitation waits on a human, so this is generous — but not
// unbounded: a handler that never returns would leak a goroutine per request
// and leave the server hanging anyway.
const inboundRequestTimeout = 5 * time.Minute

// inboundHandler answers a request the SERVER sent to us — sampling/createMessage
// and elicitation/create are the two the MCP spec defines.
//
// Returning a non-nil *rpcError refuses the request; returning both nil is
// treated as a refusal too, since a request must be answered with something.
type inboundHandler func(ctx context.Context, method string, params json.RawMessage) (any, *rpcError)

// handleInboundRequest answers one server→client request and writes the reply.
//
// Always writes exactly one reply, including for methods nothing handles: a
// JSON-RPC request that gets no response leaves the caller waiting forever, so
// "method not found" is the correct answer and silence is a hang.
//
// Runs on its own goroutine — see the call site in readLoop. Sampling calls an
// LLM and elicitation waits on a person, and the read loop is also how replies
// to our OWN calls arrive: handling this inline would deadlock the client
// against a server waiting for us.
func (c *Client) handleInboundRequest(id int64, method string, params json.RawMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), inboundRequestTimeout)
	defer cancel()

	var result any
	var rerr *rpcError
	if c.onRequest == nil {
		rerr = &rpcError{Code: rpcMethodNotFound, Message: "method not supported by this client: " + method}
	} else {
		result, rerr = c.onRequest(ctx, method, params)
		if rerr == nil && result == nil {
			rerr = &rpcError{Code: rpcMethodNotFound, Message: "method not supported by this client: " + method}
		}
	}

	reply := map[string]any{"jsonrpc": "2.0", "id": id}
	if rerr != nil {
		reply["error"] = rerr
	} else {
		reply["result"] = result
	}
	b, err := json.Marshal(reply)
	if err != nil {
		// The handler produced something unmarshalable. The server still needs
		// an answer, so send an error rather than dropping the request.
		b, _ = json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": &rpcError{Code: rpcInternalError, Message: "client could not encode its reply"},
		})
	}
	b = append(b, '\n')
	c.writeMu.Lock()
	_, werr := c.stdin.Write(b)
	c.writeMu.Unlock()
	if werr != nil {
		fmt.Fprintf(os.Stderr, "mcp: server %q: replying to %s failed: %v\n", c.name, method, werr)
	}
}

// clientCapabilities is what this client advertises at initialize.
//
// A capability is a promise: a server that sees `sampling` will send
// sampling/createMessage, and one that does not will never try. Advertising
// what the config did not enable would invite requests we are certain to
// refuse — and the spec's own guidance is that a client declares only what it
// actually serves.
func (c *Client) clientCapabilities() map[string]any {
	caps := map[string]any{}
	if c.inbound.AllowSampling && c.inbound.Sample != nil {
		caps["sampling"] = map[string]any{}
	}
	if c.inbound.AllowElicitation {
		caps["elicitation"] = map[string]any{}
	}
	return caps
}
