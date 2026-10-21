# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

A solo developer running their own projects through Orchestra's desktop/web app or the VS Code extension, on a local model (LM Studio, vLLM) or a hosted one (Claude, DeepSeek, OpenAI). They hand whole tasks to agents and review what comes back: the diffs before they are applied, the answers, and in Orchestra mode the work of a whole team of subagents.

## Product Purpose

Orchestra is a local AI coding assistant core: a model reads a project, plans edits and applies them safely. Every edit is staged in a sandbox overlay and shown as a diff before anything touches disk. Success is a developer who can give an agent a real task, follow what it is doing, and accept or reject the result with confidence.

## Positioning

Agents as an organisation. In Orchestra mode a Lead talks to the user and runs departments and roles (Product Lead, Docs Lead, Dept Leads, scouts, explorers, workers, verifiers) through phases — discovery → documentation → contract → execution → delivery — with runtime-enforced gates and every child's edits staged in its own layer until accepted. It works on local models as well as hosted ones.

## Operating Context

- One core per project over JSON-RPC (stdio for VS Code and the TUI, a WebSocket for the web and desktop app); the chat renderer is shared by the web/desktop app and the VS Code webview.
- Long turns: an orchestra turn runs for minutes with several subagents, some in parallel, some nested (a Product Lead runs its own scouts, a Dept Lead its explorers and workers).
- The developer watches live, comes back to a finished turn, and digs into failures (provider errors, cut-off answers, verification failures).
- Sessions are saved and reopened; a reopened chat must show what happened.

## Capabilities and Constraints

- Subagent events carry: task id, parent task id, depth, role (`subagent_type`), tier, model, goal, start/finish, status (`done` / `error` / `timeout`), error text, a short result summary, promote hints. Phase lives in `.orchestra/state.md` (frontmatter), changed through `update_working_state`.
- Roles are data (`internal/roles`): explore, scout, ask, product, documentation, architecture (Dept Lead with a `dept`), worker, verifier, debug, general, plus custom agents.
- Edits are dry-run by default; `--apply` / `apply: true` writes. Runtime records under `.orchestra/` change only through their own tools.
- UI must work in both light and dark themes, inside VS Code's webview (its theme variables) and in the web/desktop app.

## Product Principles

1. Show the work, not a claim about it: what an agent did, on which files, with what result.
2. Nothing reaches disk unseen: staged edits and diffs come before acceptance.
3. One conversation with the Lead; the organisation behind it stays inspectable but never noisy.
4. Works out of the box, on a local model as well as a hosted one.
