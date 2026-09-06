# MCP OAuth 2.1 for Orchestra's MCP client (Wave C, item C1)

Status: approved design, ready for implementation planning.
Plan reference: `docs/parity-plan-2026-09.md` §1.5 #6, Wave C item C1.

## Problem

Orchestra's MCP *client* (`internal/mcp`) can reach a remote MCP server only
over stdio or Streamable HTTP with, at most, a single static bearer token
(`RemoteConfig.BearerTokenEnv`, read from an env var, applied via a custom
`http.RoundTripper` in `internal/mcp/remote.go`). Real hosted MCP servers
increasingly require the MCP spec's OAuth 2.1 authorization flow
(authorization code + PKCE, Dynamic Client Registration, refresh tokens) —
Orchestra cannot connect to any of them today.

## What the SDK already gives us

Orchestra already depends on `github.com/modelcontextprotocol/go-sdk` v1.7.0
(currently an indirect dependency — this feature makes it direct). Research
into its `auth`/`oauthex` packages found the entire OAuth 2.1 client flow
already implemented, not just RFC primitives:

- `auth.AuthorizationCodeHandler` (`auth/authorization_code.go`) does the
  *whole* flow end to end: RFC 9728 protected-resource-metadata discovery
  (parses `WWW-Authenticate` on a 401), RFC 8414 authorization-server
  metadata discovery, client registration (tries Client-ID-Metadata-Document
  → preregistered client → RFC 7591 DCR, in that order), PKCE generation (via
  `golang.org/x/oauth2`), the code exchange, and RFC 9207 `iss` validation.
  It implements `auth.OAuthHandler` (`TokenSource`, `Authorize`).
- `mcp.StreamableClientTransport` has a dedicated `OAuthHandler` field
  (separate from `HTTPClient`/the bearer-token `RoundTripper` Orchestra
  already uses — the two mechanisms don't share a code path). The transport
  calls `TokenSource` to attach `Authorization: Bearer` on every request, and
  calls `Authorize(ctx, req, resp)` exactly once on a 401/403 before retrying.
- Token *persistence* is the one thing the SDK deliberately leaves to the
  caller: `AuthorizationCodeHandlerConfig.InitialTokenSource` (feed in a
  saved token) and `NewTokenSource` (a callback invoked right after a fresh
  code exchange, meant to be wrapped so refreshed tokens get saved back to
  disk — demonstrated as a documentation pattern in the SDK's own
  `auth_example_test.go`, not shipped as importable API).

**Consequence for scope:** Orchestra's own work is small — an interactive
login command, a token-persistence file format, and the plumbing to wire a
stored token into `StreamableClientTransport` for *non-interactive* runs.
Nothing about PKCE, DCR, or metadata discovery needs to be written from
scratch.

## Non-goals

- **Not** adding OAuth to the MCP *server* built in C2 (`orchestra mcp
  serve`) — that's about other tools authenticating *to* Orchestra, the
  opposite direction from this feature (Orchestra authenticating *as* a
  client to someone else's server). Separate concern, not touched here.
- **Not** implementing SEP-990 enterprise flows (`auth/extauth`:
  `ClientCredentialsHandler`, `EnterpriseHandler`, OIDC login) — v1 is the
  standard interactive Authorization-Code-with-PKCE flow only. The
  `auth.OAuthHandler` interface leaves room to add these later without
  redesigning anything.
- **Not** solving headless/SSH logins. The loopback-redirect flow
  (`orchestra mcp login` opens a browser and listens on `127.0.0.1:PORT` for
  the callback) does not work when the browser and the running process are
  on different machines. v1 documents the `ssh -L <port>:localhost:<port>`
  workaround; no device-code-style fallback is built.
- **Not** ever opening a browser from an unattended `orchestra`/`orchestra
  core` run. Interactive authorization happens *only* inside `orchestra mcp
  login`. A normal run with no valid stored token fails with a clear,
  actionable error instead of attempting to authorize — an agent turn is not
  the place for a surprise browser popup.

## Components

**New package `internal/mcpauth`** (deliberately not folding SDK-specific
`auth`/`oauthex` types into `internal/mcp` itself — keeps the OAuth-specific
surface small and swappable):

- `Login(ctx context.Context, serverName string, cfg config.MCPServerConfig) error`
  — the interactive flow: opens the user's browser to the authorization URL,
  runs a temporary loopback `http.Server` to receive the redirect (mirroring
  the SDK's own `examples/auth/client/main.go` reference pattern), drives
  `auth.NewAuthorizationCodeHandler`, and on success writes the resulting
  token to disk.
- `Logout(serverName string) error` — deletes the stored token file.
- `TokenSourceFor(serverName string) (oauth2.TokenSource, error)` — reads the
  stored token (error if absent), wraps it in the SDK's
  `oauth2.Config.TokenSource(ctx, token)` (silent access-token refresh via
  the refresh token, no browser involved) plus a save-back wrapper so a
  refreshed token is persisted immediately, not just held in memory.
- A small `oauthHandlerAdapter` implementing `auth.OAuthHandler` for
  *non-interactive* use: `TokenSource` delegates to `TokenSourceFor`;
  `Authorize` — the hook the SDK transport calls on a 401 — does **not** run
  the interactive flow; it returns an error telling the caller to run
  `orchestra mcp login <server>`. This is what enforces the "never pops a
  browser mid-turn" rule structurally, not just by convention.

**`internal/mcp/remote.go`** gains a branch: a server config with a
non-nil `oauth:` block builds `StreamableClientTransport.OAuthHandler` from
`mcpauth`'s non-interactive adapter, instead of (mutually exclusive with)
today's static-bearer-token `authTransport`/`HTTPClient` path.

## Config shape

New optional block on `config.MCPServerConfig`:

```yaml
mcp:
  servers:
    - name: linear
      url: https://mcp.linear.app
      oauth:
        client_id: ""          # optional; preregistered client. Empty = attempt DCR.
        client_secret_env: ""  # optional; env var name holding the client secret, if the preregistered client needs one.
        scopes: []             # optional; passed through to the authorization request.
```

Presence of `oauth:` (even `oauth: {}`) selects the OAuth path instead of
`bearer_token_env` for that server. Both set on the same server is a config
validation error — one auth scheme per server. `client_id` empty means "try
Dynamic Client Registration first" (matching the SDK handler's own
registration-strategy fallback order), matching the DCR-by-default,
preregistered-as-override decision made during design.

## CLI surface

Two new subcommands alongside the existing `orchestra mcp add/remove/get/
list-tools/serve`:

- `orchestra mcp login <server>` — runs `mcpauth.Login`. Prints the
  authorization URL (in case the browser doesn't auto-open, e.g. over a
  forwarded SSH port) and blocks until the loopback callback fires or a
  timeout elapses.
- `orchestra mcp logout <server>` — runs `mcpauth.Logout`.

## Token storage

`~/.orchestra/mcp-oauth/<server-name>.json`, permissions `0600` — same
plaintext-plus-restrictive-permissions posture already used for the
`orchestra core --http` debug token (`internal/cli/core.go`), chosen over an
OS keychain to avoid a new cross-platform dependency for a security posture
the project already accepts elsewhere. Global (`~/.orchestra/`), not
per-project (`.orchestra/`) — an OAuth grant belongs to the user's identity
with that remote service, not to any one project, matching how provider API
keys already live in `~/.orchestra/config.yml` rather than per-project.
`<server-name>` is taken from the MCP server's configured `name` — the
implementation must sanitize it against path traversal before building the
file path (never interpolate a config string into a filesystem path
unchecked), even though `orchestra mcp add` already constrains server names
elsewhere.

File contents: enough to reconstruct an `oauth2.Token` (access token, token
type, refresh token, expiry) plus the server's authorization-server issuer
URL (needed to rebuild the `oauth2.Config` for a silent refresh without
re-running discovery every process start).

## Token lifecycle & error handling

- **Normal run** (`orchestra`, `orchestra core`, `orchestra mcp serve`'s own
  outbound MCP client connections, etc.): `TokenSourceFor` loads the stored
  token. An expired access token is silently refreshed via the refresh token
  (network call, no browser) and the refreshed token is written back to
  disk. No prompt, no browser, ever.
- **No stored token, or refresh itself fails** (revoked/expired refresh
  token): the connection attempt fails with a specific, actionable error —
  `mcp server "<name>": not authenticated, run: orchestra mcp login <name>`
  — surfaced the same way other MCP server startup failures already are
  (`core.mcpStartErrs`), not a generic connection error.
- **`orchestra mcp login <name>`**: runs the full interactive
  `auth.AuthorizationCodeHandler` flow. On success, overwrites any existing
  token file for that server. Errors (server doesn't support OAuth per its
  metadata, DCR rejected and no preregistered `client_id` configured, user
  closes the browser without completing) are reported directly to the
  terminal, non-zero exit.
- **`orchestra mcp logout <name>`**: deletes the token file. Idempotent — no
  error if nothing was stored.

## Testing

- `internal/mcpauth`: unit tests against a fake authorization server (the
  SDK ships exactly this test fixture — `internal/oauthtest/fake_authorization_server.go`
  — reusable in Orchestra's own tests the same way the SDK's own tests use
  it) covering token save/load, silent refresh-and-persist, and the
  non-interactive adapter's `Authorize` returning the "run mcp login" error
  rather than attempting a browser flow.
- `internal/mcp`: a config-validation test that `oauth:` and
  `bearer_token_env` together on one server is rejected.
- CLI: `orchestra mcp login`/`logout` flag/wiring tests following the same
  pattern as `mcp serve`'s own CLI tests — real logic (real temp
  `~/.orchestra` override via env, real fake-auth-server round trip for
  `login`), not mocks.
- No test exercises a real browser open — `Login`'s browser-launch step is
  behind a small seam (an injectable "open URL" function) so tests can
  supply a fake that just records the URL instead of shelling out.

## Risks / open items for the implementation plan

1. Confirm the exact SDK type/field names for constructing
   `AuthorizationCodeHandlerConfig` end to end (redirect URL shape, how
   `AuthorizationCodeFetcher` is expected to signal the received code) by
   reading `examples/auth/client/main.go` line by line — the research pass
   summarized its shape, but the implementation plan needs the literal
   signatures before writing tasks.
2. Confirm whether `config.MCPServerConfig` validation (one auth scheme per
   server) belongs in the existing config-loading validation pass or needs a
   new dedicated check — depends on where `bearer_token_env` itself is
   currently validated.
3. Decide the exact on-disk JSON shape (a hand-rolled struct vs. wrapping
   `oauth2.Token` directly) during implementation — no user-facing behavior
   difference, pure implementation detail.
