package agent

import (
	"fmt"
	"os"
	"runtime/debug"
)

// safeRun executes fn with a deferred recover() so a panic inside fn does
// not kill the whole process. Returns any recovered value (or nil) AND
// writes the panic + stack to os.Stderr labelled with `label` so the
// operator can correlate the failure with its subsystem.
//
// Use for void callbacks where the contract is "fire-and-forget" — event
// sinks, audit loggers, progress hooks.
//
// C3 in docs/superpowers/plans/2026-05-19-post-audit-refactor.md: before
// this, a panic in any tool / hook / OnEvent killed the goroutine — and
// inside runParallelToolBatch that goroutine is a fan-out worker, so a
// single buggy tool/hook took down the whole process.
//
// N7 in audit ledger (Sprint 6): every existing call site used the form
// `_ = safeRun(...)`, silently swallowing panics — the operator never
// saw why an OnEvent hook stopped firing. Logging from inside safeRun
// removes that footgun.
func safeRun(label string, fn func()) (recovered any) {
	defer func() {
		recovered = recover()
		if recovered != nil {
			fmt.Fprintf(os.Stderr, "agent.safeRun: %s panicked: %v\n%s\n", label, recovered, debug.Stack())
		}
	}()
	fn()
	return
}

// safeRunErr is the error-returning variant: fn returns its own error,
// safeRunErr converts a panic into a synthetic error so the caller can
// treat panic and error uniformly (logging, circuit-breaker accounting,
// retry decisions). The synthetic error embeds the recovered value and
// a stack snippet for debuggability.
//
// Used for hook calls (HooksRunner.RunPreTool / RunPostTool) and any
// tool-dispatch wrapper where the calling pipeline already has an error
// branch — converting panic→error means we don't have to introduce a
// new control-flow path.
func safeRunErr(label string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s panicked: %v\n%s", label, r, debug.Stack())
		}
	}()
	return fn()
}
