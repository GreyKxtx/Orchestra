package rpcclient

import (
	"errors"
	"fmt"
	"testing"

	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

// A core before ProtocolVersion 24 refuses any version but its own and
// names it; the TUI asks again for that version when it still speaks it.
func TestOlderCoreVersion(t *testing.T) {
	mismatch := func(core any) error {
		return fmt.Errorf("rpc: %w", &jsonrpc.RPCError{
			Code:    protocol.ProtocolMismatch.RPCCode(),
			Message: "protocol_version mismatch",
			Data:    map[string]any{"client": float64(protocol.ProtocolVersion), "core": core},
		})
	}
	if v, ok := olderCoreVersion(mismatch(float64(protocol.MinProtocolVersion))); !ok || v != protocol.MinProtocolVersion {
		t.Fatalf("the previous release's core: %d %v", v, ok)
	}
	if _, ok := olderCoreVersion(mismatch(float64(protocol.MinProtocolVersion - 1))); ok {
		t.Fatal("a core older than the window is not retried")
	}
	if _, ok := olderCoreVersion(mismatch(float64(protocol.ProtocolVersion + 1))); ok {
		t.Fatal("a core newer than this client speaks negotiated already; a refusal from it is final")
	}
	if _, ok := olderCoreVersion(mismatch("23")); ok {
		t.Fatal("a version that is not a number is not retried")
	}
	other := &jsonrpc.RPCError{Code: protocol.AlreadyInitialized.RPCCode(), Data: map[string]any{"core": float64(protocol.MinProtocolVersion)}}
	if _, ok := olderCoreVersion(other); ok {
		t.Fatal("only ProtocolMismatch is a version refusal")
	}
	if _, ok := olderCoreVersion(errors.New("connection closed")); ok {
		t.Fatal("a transport error is not retried")
	}
	if _, ok := olderCoreVersion(nil); ok {
		t.Fatal("nil")
	}
}
