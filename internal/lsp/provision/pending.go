package provision

import "errors"

// ErrEnsurePending means a language-server install is still running in the
// background. Callers (diagnostics) should soft-fail with empty results and
// retry on the next tool call.
var ErrEnsurePending = errors.New("lsp: language server install in progress")

// IsEnsurePending reports whether err (or a wrapped err) is ErrEnsurePending.
func IsEnsurePending(err error) bool {
	return errors.Is(err, ErrEnsurePending)
}
