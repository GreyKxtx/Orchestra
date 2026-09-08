// Package llmauth obtains bearer credentials for LLM providers: OAuth 2.0
// (authorization code with PKCE, or RFC 8628 device authorization) against
// endpoints the user configures, and an external token_command helper.
//
// Orchestra ships no provider presets. The endpoints and client identity in
// an auth block come from the user's own registered application or their
// organization's gateway: shipping a client_id lifted from a vendor's own
// application would mean presenting as that application, which is a
// different thing from authorizing with OAuth. See
// docs/superpowers/specs/2026-09-08-llm-oauth-design.md, "Non-goals".
package llmauth

// Namespace is the ~/.orchestra subdirectory holding LLM provider tokens.
// internal/mcpauth uses "mcp-oauth" for the same purpose; both go through
// internal/authstore.
const Namespace = "llm-oauth"
