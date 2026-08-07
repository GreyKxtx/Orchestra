// Package permission carries the shared shape of an interactive consent
// request between the agent loop and the core RPC handler.
//
// Previously the same Requester / Request / Response trio was declared
// twice — once in internal/core (the RPC side) and again in internal/
// agent (so the agent could be called without depending on core). An
// agentRequesterAdapter bridged them, copying values field-for-field.
// Moving the types here lets both sides depend on one declaration and
// removes the adapter altogether (H6 in architecture audit).
package permission

import "context"

// Requester asks the connected client (TUI / IDE / agent harness) for
// interactive consent before running a sensitive tool (e.g. exec.run /
// bash). Implementations live in core for the JSON-RPC client-callback
// path and in tests for fixtures; the agent only consumes the interface.
type Requester interface {
	RequestPermission(ctx context.Context, req Request) (Response, error)
}

// Request describes the tool action requiring consent.
type Request struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	Reason      string `json:"reason,omitempty"`
	// Kind distinguishes consent flows: ""/"exec" (shell) vs "lsp.install".
	Kind string `json:"kind,omitempty"`
}

// Response is the client's consent decision.
type Response struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
	// Always means "remember for this session / persist auto policy".
	// For kind=lsp.install, TUI maps this to lsp.auto_install=true.
	Always bool `json:"always,omitempty"`
}
