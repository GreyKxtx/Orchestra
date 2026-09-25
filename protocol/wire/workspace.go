package wire

// workspace.trust_status, workspace.trust.

// WorkspaceTrustParams is the workspace.trust request. Revoke forgets the
// workspace instead of trusting it.
type WorkspaceTrustParams struct {
	Revoke bool `json:"revoke,omitempty"`
}
