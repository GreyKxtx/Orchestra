# Orchestra as an MCP server (Wave C, item C2)

Status: approved design, ready for implementation planning.
Plan reference: `docs/parity-plan-2026-09.md` §1.5 #8, Wave C item C2.

## Problem

Orchestra is currently an MCP *client* only (`internal/mcp` — stdio + Streamable
HTTP transports, tool/resource/prompt support, multi-server `Manager`). Nothing
in Orchestra can be reached *by* another MCP-capable tool (Claude Code, Claude
Desktop, Cursor, …).

Orchestra has built real code-intelligence machinery most competitors don't
have out of the box: a multi-hop code knowledge graph (CKG v5, `explore` with
depth/direction), semantic (embedding) search, an OpenTelemetry-trace-to-code
resolver (`runtime_query`), and a managed LSP layer (`lsp.*`: definition,
references, hover, diagnostics, rename). Today all of it is locked inside
Orchestra's own agent loop. The plan doc flags this as the one item where
shipping it would put Orchestra *ahead* of competitors rather than merely at
parity — but requires an API design pass before any code, which is what this
document is.

## Non-goals

- **Not** a general-purpose remote-execution surface. No `write`/`edit`/`bash`/
  `exec` — read-only code intelligence only (see Tool surface below).
- **Not** a second daemon. `internal/daemon`/`orchestra daemon` (v0.3 HTTP) was
  removed (`fdce9a3`) because it duplicated `orchestra core`'s job with a
  weaker safety story (forced-loopback bind, its own discovery/state-cache
  layer, no real auth). This design deliberately reuses `orchestra core`'s
  existing config-loading and lifecycle instead of inventing a parallel one,
  and requires real auth before allowing any non-loopback bind. `internal/daemon`
  stays CI-banned (`tests/importrules` `TestNoLegacyInternalSubmodules`) and
  nothing here reintroduces it.
- **Not** MCP OAuth 2.1 (plan item C1, separate, not built yet). HTTP auth here
  is a static bearer token, matching the precedent `orchestra core --http`
  already sets (`internal/cli/core.go:87-90`, `jsonrpc.StartHTTP`). OAuth can
  layer on top later as an additional supported mode without breaking this one.
- **Not** multi-workspace-per-process. One `orchestra mcp serve` process is
  bound to one workspace root for its whole lifetime, for both transports.
  Serving several projects means running several processes (different ports
  for HTTP). This matches `orchestra core`'s own model and avoids any
  possibility of one caller's tool call leaking into another project's CKG
  index.

## Tool surface

Ten tools, all already implemented and already agent-independent (each has a
small, dependency-injected Go client requiring no LLM/session state):

| MCP tool name | Go method | Client |
|---|---|---|
| `explore` | `Runner.ExploreCodebase` | `internal/tools/nav` |
| `semantic_search` | `Runner.SemanticSearch` | `internal/tools/nav` |
| `symbols` | `Runner.CodeSymbols` | `internal/tools/nav` |
| `repo_map` | `Runner.RepoMap` | `internal/tools/nav` |
| `runtime_query` | `Runner.RuntimeQuery` | `internal/tools/session` |
| `lsp.definition` | `Runner.LSPDefinition` | `internal/tools/toolslsp` |
| `lsp.references` | `Runner.LSPReferences` | `internal/tools/toolslsp` |
| `lsp.hover` | `Runner.LSPHover` | `internal/tools/toolslsp` |
| `lsp.diagnostics` | `Runner.LSPDiagnostics` | `internal/tools/toolslsp` |
| `lsp.rename` | `Runner.LSPRename` | `internal/tools/toolslsp` |

Deliberately excluded: `RunCKGEmbed`/`RebuildCKG` (index-mutating; a caller
that needs a fresh index can be told to run `orchestra` normally first) and
everything outside code-intelligence (`write`, `edit`, `bash`, `exec`, `git.*`,
`web.*`, memory tools, task/subagent tools) — those are agent-loop concerns,
not "expose Orchestra's code understanding," and expanding scope here would
turn this into the daemon this design explicitly avoids re-creating.

**Schema reuse:** each tool's `mcpsdk.Tool.InputSchema` is set directly from
the existing `llm.ToolDef` the agent already uses (e.g.
`nav.ToolExploreCodebase().Function.Parameters`, a `json.RawMessage` built via
`toolschema.MustSchema`) — `InputSchema` is declared `any` in the SDK and is
remarshaled generically when it isn't a typed `*jsonschema.Schema`, so the
existing raw JSON plugs in with no translation step to keep in sync. Tool
`Description` is copied the same way. This means the schema and description an
external MCP client sees are always identical to what Orchestra's own LLM
provider sees — one source of truth, verified by a test that iterates both
tool tables and diffs name/description/schema.

**Naming risk to verify early in implementation:** the `lsp.*` tool names
contain a dot. Some LLM providers' function-calling name validation is
stricter than MCP's; Orchestra already ships these names successfully through
its own agent loop against real providers, so this is very likely a
non-issue for MCP (which has no such restriction), but the first implementation
task should confirm at least one real MCP host (e.g. Claude Code's own
`mcp__<server>__<tool>` convention) round-trips a dotted tool name before
building anything on top of the assumption.

## Process & package architecture

Builds on `internal/core`, not a freestanding package or `tools.Runner` used
directly — `internal/core` already owns exactly this kind of surface
(`core/rpc_handler.go`, `core/core_agent.go`), and its private `tools *tools.Runner`
field is the thing being exposed, so the wiring belongs where that field
lives rather than behind a new exported accessor.

**`core.Options` gains `ToolsOnly bool`.** When set, `core.New` skips:
- LLM client construction,
- the network call to resolve model context-window limits (`llm.ResolveModelLimits`, 8s timeout),
- starting Orchestra's own configured MCP *client* servers (`internal/mcp.Manager`).

None of the ten tools need any of these three things to run, and requiring
them would mean `orchestra mcp serve` can't start at all without a working,
reachable LLM endpoint configured — an unnecessary and surprising coupling for
a tool-only server. Every other `core.New` caller (`orchestra core`,
`apply --via-core`, `orchestra eval`) leaves the flag unset and is unaffected.

**New file `internal/core/core_mcp_server.go`**: `func (c *Core) MCPToolServer() *mcpsdk.Server`
builds an `mcpsdk.NewServer` and registers the ten tools above via
`mcpsdk.AddTool`, each handler calling straight into `c.tools.<Method>`.

**New file `internal/cli/mcp_serve.go`**: `orchestra mcp serve`, a new
subcommand under the existing `mcpCmd` group (alongside `list-tools`, `add`,
`remove`, `get`).

```
orchestra mcp serve --workspace-root <path>
    [--http] [--http-addr 127.0.0.1:PORT] [--mcp-token TOKEN | $ORCH_MCP_TOKEN]
```

- No `--http`: serve over stdio (default), matching how MCP hosts configure
  local servers (a command + args) and how `orchestra core` itself defaults.
- `--http`: Streamable HTTP. Bind address defaults to `127.0.0.1` (loopback);
  binding anywhere else requires explicitly passing a non-loopback
  `--http-addr`, mirroring the MCP *client*'s existing symmetric rule
  ("plaintext `http://` to a non-loopback host is rejected unless
  `allow_insecure_http: true`", README.md's MCP config section) — local-safe
  by default, explicit opt-in to expose it.
- `--mcp-token` / `ORCH_MCP_TOKEN` is **mandatory whenever `--http` is set**,
  at any bind address — the command refuses to start without one. Unlike
  `orchestra core --http` (debug-only, auto-generates a token if omitted, has
  no remote-use ambition), this mode's whole point is real remote/multi-client
  use, so silently generating a token nobody was told about is the wrong
  default here. Checked via a bearer-auth middleware wrapping
  `mcpsdk.NewStreamableHTTPHandler`.

## Response format & error handling

Success: marshal the response struct to JSON, return as MCP text content —
identical to the JSON shape these tools already produce for the agent loop
today, so nothing new to document for a caller who already knows Orchestra's
tool outputs from its own docs.

Failure: MCP's own tool-error convention (`IsError: true` on the tool result)
covers both a runtime failure such as "no LSP servers configured" and
malformed input that fails schema validation — per the MCP Go SDK (v1.7.0)
actually in use, schema-validation failures for a tool call also come back
as `IsError: true` on the `CallToolResult`, not as a JSON-RPC-level error;
only a handler explicitly returning a `*jsonrpc.Error` takes that different
path, which none of these ten tools do. Orchestra's internal
recoverable-error hints (`StaleContent`, `AmbiguousMatch`) are not surfaced —
those exist so Orchestra's *own* agent can self-correct mid-turn; an external
MCP client has no such retry contract and should just see a plain error.

## Testing

- `internal/core/core_mcp_server_test.go` — construct `Core` with
  `ToolsOnly: true` against a fixture repo (real files, real CKG index, no
  mocks), obtain `c.MCPToolServer()`, and drive it through a real in-process
  MCP client the same way `internal/mcp/remote_test.go` already exercises the
  client side (`mcpsdk.NewClient` against an in-memory or `httptest` transport)
  — one test per tool, asserting on real output from the fixture repo, plus a
  schema-parity test (server tool defs vs. the agent's own `llm.ToolDef` table).
- `internal/cli/mcp_serve_test.go` — flag parsing; `--http` without a token
  refuses to start; default bind stays loopback; a non-loopback `--http-addr`
  without an accompanying explicit acknowledgement is rejected the same way
  the MCP client config rejects it today.
- Nothing in this path touches a live LLM — `ToolsOnly: true` guarantees that
  structurally, not just by test discipline.

## Risks / open items for the implementation plan

1. Verify the dotted `lsp.*` tool names round-trip cleanly through at least
   one real MCP host before assuming the naming is fine as-is (see Tool
   surface above).
2. `mcpsdk.AddTool[In, Out]` is generic over typed Go request/response structs;
   confirm the exact mechanics of overriding its auto-generated schema with
   the existing raw `toolschema.MustSchema` JSON (expected to work via the
   SDK's non-typed-schema remarshal path, per `mcp/server.go`, but this needs
   a real failing test before being treated as settled).
3. Decide the exact bearer-auth middleware shape for the HTTP path (wrapping
   `mcpsdk.NewStreamableHTTPHandler` vs. a `net/http` middleware in front of
   it) during implementation — both are viable, no user-facing behavior
   difference.
