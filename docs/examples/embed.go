// Package examples embeds the example files under docs/examples so runtime
// code (orchestra init) reads the exact same template a human finds in the
// repo, instead of a second copy that could silently drift from it.
package examples

import _ "embed"

// OrchestraTemplate is the source `orchestra init` fills in and writes to
// ORCHESTRA.md on a fresh project. See ORCHESTRA.template.md in this
// directory for the human-readable copy.
//
//go:embed ORCHESTRA.template.md
var OrchestraTemplate string
