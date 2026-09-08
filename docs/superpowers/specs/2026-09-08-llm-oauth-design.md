# OAuth and external token sources for LLM providers (Wave C, item C5)

Status: approved design, ready for implementation planning.
Plan reference: `docs/parity-plan-2026-09.md` §1.6 #7, Wave C item C5.

## Problem

Every LLM endpoint Orchestra talks to authenticates with one static string:
`LLMConfig.APIKey`, read from `.orchestra.yml` (or `.orchestra.local.yml`,
or `ORCH_API_KEY`) and frozen into the client at construction
(`llm/client.go:124`, `llm/anthropic.go:58`). That leaves two real setups
unreachable:

- Endpoints that authenticate with a **short-lived bearer** rather than a
  permanent key — a corporate SSO gateway in front of a model service, a
  self-registered OAuth application, a cloud endpoint whose token comes
  from `gcloud auth print-access-token`. Their tokens expire in minutes to
  hours; a static string in a config file cannot track that.
- Anyone who does not want a long-lived secret sitting in a project file at
  all.

The plan line frames this as "cheap work without API keys — the thing
people love OpenCode for", naming Claude Pro/Max, ChatGPT and Copilot. That
specific framing is explicitly **not** what this design delivers; see
Non-goals.

## What already exists

`internal/mcpauth` shipped with Wave C item C1 and implements OAuth 2.1 for
Orchestra's MCP *client*. Read honestly, it splits unevenly:

- `login.go` (209 lines) is MCP-specific end to end. It drives the flow
  *through* `mcpsdk.Client.Connect` with an `auth.OAuthHandler`, so metadata
  discovery starts from the MCP server's own RFC 9728 protected-resource
  document. An LLM provider has no MCP server to discover from, so none of
  this transfers.
- `handler.go` (44 lines) is likewise an `auth.OAuthHandler` implementation
  for the MCP transport.
- `token.go` (113 lines) and `tokensource.go` (89 lines) are generic OAuth
  token persistence. The only MCP-specific parts are the hardcoded
  `~/.orchestra/mcp-oauth/` directory and the wording of error messages.
- `oauthtest/fake_authorization_server.go` (294 lines) is a real fake
  authorization server used by round-trip tests. It is about OAuth, not
  about MCP.

`tokensource.go:41-47` carries a subtle correctness guard: some
authorization servers omit `refresh_token` from a refresh response, meaning
the original refresh token stays valid and must be reused. Dropping it
silently turns every later refresh into an authentication failure. A guard
like that must exist exactly once in the codebase.

Two further facts shape the design:

- `llm/factory.go:14` is the **only** non-test construction site for
  `NewOpenAIClient`, and `config.LLMConfig` is a type alias for
  `llmpkg.LLMConfig` (`internal/config/config.go:18`). Anything added to
  that one struct is visible to every config path and every one of the 20+
  `llm.NewClient` call sites without touching them.
- `llm/go.mod` declares **no dependencies at all**. Keeping it that way is a
  design constraint, not an accident.

## Non-goals

- **No presets that impersonate a first-party application.** Orchestra will
  not ship `client_id` values lifted from Claude Code, the ChatGPT desktop
  app, or any other vendor client in order to use a consumer subscription.
  Sending another application's client identity is not "OAuth with a
  subscription"; it is presenting as that application. The configuration
  below is deliberately generic, so a user who supplies such values in their
  own file can do so — that is their decision and their risk. What Orchestra
  ships in a public MIT repository is a different question, and the answer
  here is no.
- **No user accounts in Orchestra.** This design makes Orchestra an OAuth
  *client* to somebody else's authorization server. Orchestra issuing or
  holding accounts of its own is a separate project and is not in scope.
- **No RPC method.** Login is interactive and terminal-bound, exactly like
  `orchestra mcp login` from C1. The TUI and the VS Code extension see the
  result through the ordinary config path; neither needs a new protocol
  method, so the protocol version does not move.
- **No credential storage in the OS keychain.** File permissions 0600 under
  the user's home directory match what C1 already does. Revisit only if a
  user asks.

## Config shape

A provider block gains an optional `auth:` key carrying exactly one
mechanism. Both present is a configuration error, reported at load time
with the provider's name.

```yaml
providers:
  corp-gateway:
    api_base: https://llm.corp.example/v1
    model: gpt-4o
    auth:
      oauth:
        auth_url:        https://sso.corp.example/authorize
        token_url:       https://sso.corp.example/token
        device_auth_url: https://sso.corp.example/device   # flow: device only
        client_id:       orchestra-cli
        scopes:          [llm.invoke, offline_access]
        flow:            browser        # browser | device (default: browser)
        extra_params:                   # optional, sent on the auth request
          audience: https://llm.corp.example

  vertex:
    api_base: https://europe-west4-aiplatform.googleapis.com/v1
    model: gemini-2.5-pro
    auth:
      token_command: ["gcloud", "auth", "print-access-token"]
      token_command_ttl: 45m            # default 5m when omitted
```

`client_secret` is **not** a field of this block. A confidential client
reads its secret from `.orchestra.local.yml` or the environment, matching
how every other secret in this project is handled: `Save` masks keys defined
in `.orchestra.local.yml` back to their on-disk values before writing, so a
secret supplied through the overlay never leaks into the shared
`.orchestra.yml` (`internal/config/config.go:877-880`). The `auth:` block
itself — endpoints, client id, scopes — is not secret and persists normally.

`flow: browser` runs authorization-code with PKCE against a loopback
redirect — the same shape C1 proved. `flow: device` runs RFC 8628 device
authorization, which is what a headless or SSH session needs;
`device_auth_url` is required in that case and its absence is a config
error.

## Components

**`internal/authstore` (new).** The generic half extracted from
`internal/mcpauth`:

- `Token` — the on-disk grant (`token_url`, `client_id`, `client_secret`,
  `access_token`, `token_type`, `refresh_token`, `expiry`).
- `Save(dir, name string, tok Token) error`, `Load(dir, name string)
  (Token, error)`, `Delete(dir, name string) error`, all keyed by a
  namespace directory under `~/.orchestra/` plus a single path component,
  with `ErrNoToken` for "nothing stored". The existing name validation
  (rejecting both separators explicitly on every platform, `.` and `..`)
  moves across unchanged — CI caught that one on Linux and the reasoning is
  recorded in `token.go:41-55`.
- `PersistingTokenSource` — wraps an `oauth2.TokenSource`, writes a
  refreshed token straight back to disk, and carries the omitted-
  `refresh_token` guard. This is the single copy.

Writes stay atomic via `patch/fsutil.AtomicWriteFile` at 0600.

**`internal/mcpauth` (modified).** Becomes a consumer of `authstore` with
namespace `mcp-oauth`. Its own `Token`, `SaveToken`, `LoadToken`,
`DeleteToken` and `persistingTokenSource` are removed in favour of the
shared ones; `login.go` and `handler.go` are otherwise untouched. Its
existing tests are the regression proof that C1 still works.

**`internal/oauthtest` (moved).** `internal/mcpauth/oauthtest` relocates
here unchanged. It is an OAuth test double, and both auth packages need it.
The move is mechanical: package path only.

**`internal/llmauth` (new).** The LLM-provider half, built on plain
`golang.org/x/oauth2` v0.35.0 (already a root-module dependency):

- `Login(ctx, cfg LoginConfig) error` — browser flow via `AuthCodeURL` with
  PKCE and a loopback receiver, or device flow via `Config.DeviceAuth` and
  `Config.DeviceAccessToken` (both present in v0.35.0). Persists through
  `authstore` under namespace `llm-oauth`. This is the only path that opens
  a browser or prints a user code.
- `Logout(name string) error` — idempotent.
- `TokenSourceFor(ctx, name string, oc OAuthConfig) (oauth2.TokenSource,
  error)` — silent refresh, no prompting; returns an actionable error
  naming `orchestra auth login <name>` when nothing is stored.
- `commandTokenSource` — runs `token_command` and caches stdout for
  `token_command_ttl`. Trailing whitespace is trimmed; a non-zero exit or
  empty output is an error carrying the command's stderr.
- `Attach(name string, cfg *llm.LLMConfig) error` — reads `cfg.Auth` and
  sets `cfg.TokenSource`. No-op when `cfg.Auth` is nil.

## Plumbing into the LLM client

`llm.LLMConfig` gains two fields:

- `Auth *AuthConfig` with a `yaml:"auth,omitempty"` tag — plain data
  (endpoints, client id, scopes, flow, command, ttl). Data only, so
  `llm/go.mod` acquires no dependency.
- `TokenSource func() (string, error)` with `yaml:"-"` — set by the config
  layer, invisible to YAML on both read and write.

Provider resolution is spread across five sites
(`internal/config/config.go:637`, `internal/core/runtime_llm.go:74,170,229`,
`internal/core/runtime_providers.go:180`), so attaching there would be
forgotten by the sixth. Attachment happens **once, at config load**: walk
`cfg.LLM` and every entry of `cfg.Providers`, and call `llmauth.Attach` for
each one carrying `Auth`. `LLMConfig` travels by value and the function
field copies with it, so every downstream resolution and every
`llm.NewClient` call inherits it. The one place that needs a matching edit
is the inheritance branch that copies `APIKey` down from `llm:`
(`internal/config/config.go:552-565`) — `TokenSource` inherits on the same
terms.

Five consumption points, each resolving the token source first and falling
back to the static `APIKey`:

1. `(*OpenAIClient).setAuthHeader` (`llm/azure.go:62`) — covers both request
   paths (`llm/client.go:918` and `:1088`). Its signature changes to return
   `error` so a refresh failure surfaces instead of silently sending no
   credential; both call sites propagate.
2. `AnthropicClient`'s `x-api-key` header (`llm/anthropic.go:178`).
3. `DiscoverModelLimits` as called from `(*OpenAIClient).DiscoverAndApplyLimits`
   (`llm/client.go:172`) — it rebuilds an `LLMConfig` from client fields and
   must carry the token source across.
4. `Probe` (`llm/probe.go:37`) — already takes a whole `LLMConfig`, so this
   is one resolve-first line.
5. `Credits` (`llm/credits.go:42`) — same shape as `Probe`.

Items 4 and 5 need no new plumbing precisely because they accept
`LLMConfig` rather than a bare key string.

## CLI surface

`internal/cli/auth.go` (today 114 lines: `list`, `set-key`) gains:

- `orchestra auth login <provider>` — runs the configured flow. Prints a
  one-line notice before starting: the user is responsible for complying
  with the provider's terms of service. No gating flag; the notice is
  proportionate to a design that carries no impersonation presets.
- `orchestra auth logout <provider>` — deletes the stored token, idempotent.
- `orchestra auth list` — extended to report the authentication mechanism
  and token state per provider: `oauth (expires in 42m)`, `oauth (expired,
  run: orchestra auth login X)`, `token_command`, or the existing redacted
  `key=sk…ab`. A provider with no credential at all is shown as such rather
  than omitted.

`login` and `logout` reject a provider name absent from config, and a
provider present but without an `auth:` block, each with a message naming
what to add.

## Token lifecycle and error handling

A stored token refreshes silently and transparently: `TokenSourceFor`
rebuilds an `oauth2.Config` from the persisted `token_url`/`client_id`
without re-running discovery, and `PersistingTokenSource` writes the result
back immediately so the next process start finds a live token.

When a refresh fails mid-run — expired refresh token, revoked grant,
authorization server down — the request fails with an actionable error
naming `orchestra auth login <provider>`. Orchestra never opens a browser
outside an explicit `login`, mirroring the rule `mcpauth/login.go:100-104`
already states for MCP. `token_command` failures behave the same way, with
the command's stderr included.

Concurrency: each provider is a separate file, so two providers refreshing
at once do not contend. Two Orchestra processes refreshing the *same*
provider can interleave; the atomic write means the loser is overwritten,
not corrupted, and both tokens are valid grants, so the practical effect is
one wasted refresh.

## Testing

Round-trip tests against `internal/oauthtest`'s fake authorization server,
following the C1 pattern of exercising real code rather than mocks:

- Browser flow: `Login` with an injected `OpenURL` that visits the URL
  → token on disk with the expected fields.
- Device flow: `Login` against the fake server's device endpoints,
  including the `authorization_pending` poll before success.
- Silent refresh: seed an expired access token with a valid refresh token,
  take a token source, observe a fresh access token **and** that the new
  token was persisted.
- The omitted-`refresh_token` case: fake server refreshes without returning
  a refresh token; the stored token must keep the original one and a second
  refresh must still succeed.
- `token_command`: a real `exec` against a test binary — success, TTL
  caching (second call inside the TTL does not re-run the command), non-zero
  exit surfacing stderr, empty output rejected.
- Header plumbing: `setAuthHeader` with a token source set prefers it over
  `APIKey`, falls back when unset, and returns the source's error.
- Config: `auth:` with both mechanisms is rejected at load; `flow: device`
  without `device_auth_url` is rejected; `TokenSource` is attached to
  `cfg.LLM` and to each named provider, and inherits from `llm:` on the same
  terms as `APIKey`.
- `internal/mcpauth`'s existing tests must pass unchanged after the
  extraction — that is the whole point of doing it as an extraction.

Per project practice, non-trivial test claims are mutation-verified: break
the production line, watch that specific test fail, restore.

## Risks and open items for the implementation plan

- **The `setAuthHeader` signature change is the one edit on the hot path of
  every request**, in a separate Go module that every client links. It is
  five call sites including tests, but it ships to everyone, including users
  who will never configure `auth:`. The mitigation is that the fallback path
  is unchanged: no `TokenSource`, no behaviour change.
- The extraction touches `internal/mcpauth`, which shipped days ago. Its
  tests are the safety net; if any of them needs editing to pass, that is a
  signal the extraction changed behaviour and must be re-examined rather
  than papered over.
- `token_command` executes a user-configured program. It runs with the
  user's own privileges from their own config file — the same trust level as
  every other command Orchestra's config can name — but the plan should
  place the TTL cache carefully so a slow helper is not invoked per request.
- Device flow's polling interval must honour the server's `interval` and
  `slow_down` responses; getting this wrong produces rate-limit errors that
  look like auth failures.
