// Package examples embeds the example files under docs/examples so runtime
// code (orchestra init) reads the exact same template a human finds in the
// repo, instead of a second copy that could silently drift from it.
package examples

import "embed"

// OrchestraTemplate is the source `orchestra init` fills in and writes to
// ORCHESTRA.md on a fresh project. See ORCHESTRA.template.md in this
// directory for the human-readable copy.
//
//go:embed ORCHESTRA.template.md
var OrchestraTemplate string

// l0PlaybooksFS holds every shipped L0 default playbook (playbooks/*.md).
// This directory only exists in Orchestra's own repo checkout — a target
// project has no way to fs.read it directly, so orchestra init materializes
// L0Playbooks() into .orchestra/playbooks/l0/ instead.
//
//go:embed playbooks/*.md
var l0PlaybooksFS embed.FS

// L0Playbooks returns every shipped L0 default playbook, keyed by filename
// (e.g. "go_engineering.md"). See playbooks/README.md in this directory for
// what each one covers.
func L0Playbooks() map[string]string {
	entries, err := l0PlaybooksFS.ReadDir("playbooks")
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := l0PlaybooksFS.ReadFile("playbooks/" + e.Name())
		if err != nil {
			continue
		}
		out[e.Name()] = string(data)
	}
	return out
}
