// Package daemon implements the legacy HTTP v0.3 project cache server.
//
// Deprecated: the supported transport is JSON-RPC over stdio via `orchestra core`.
// The `orchestra daemon` CLI command was removed; this package remains only for
// in-repo benchmarks (`tests/benchmark`) and migration reference. Do not build
// new features on it. See docs/ROADMAP.md and docs/architecture/modules.md.
package daemon
