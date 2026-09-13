package agent

import (
	"strings"

	"github.com/orchestra/orchestra/patch/patches"
)

// A patch whose declared type disagrees with the fields it carries states its
// intent twice, and the fields are the honest half.
//
// The model that broke two_files sent this:
//
//	{"path":"util.go","type":"file.write_atomic",
//	 "search":"","replace":"package main\n\nfunc Double(n int) int {…"}
//	{"path":"main.go","type":"file.write_atomic",
//	 "search":"package main\n\nfunc main() {\n}\n","replace":"…import \"fmt\"…"}
//
// Both name file.write_atomic and both fill in the fields of a partial edit,
// so Content is empty. Taken literally that writes two empty files, which is
// what it did until the fs layer learned to refuse it. But refusing costs the
// whole turn: the model read the refusal and sent the same patch five times
// until the breaker ended the run.
//
// The intent is not ambiguous. An empty search means "this is the whole file"
// and a filled-in search means "replace this text with that" — the same two
// things the two patch types mean. So read the fields and fix the label.
//
// This belongs here, at the edge where model output is parsed, and not in the
// fs layer: the storage side stays strict, and its refusal remains as the
// backstop for anything this does not straighten out.
func normalizeFinalPatch(p patches.Patch) (patches.Patch, string) {
	switch p.Type {
	case patches.TypeFileWriteAtomic:
		if p.Content != "" || p.Replace == "" {
			return p, ""
		}
		if p.Search == "" {
			// Nothing to anchor to, so replace is the whole file — which is
			// what write_atomic means. Only the field is wrong.
			p.Content = p.Replace
			p.Replace = ""
			return p, "whole-file content was in replace"
		}
		p.Type = patches.TypeFileSearchReplace
		return p, "declared write_atomic but carries a partial edit"

	case patches.TypeFileSearchReplace:
		if p.Search != "" || p.Replace != "" || p.Content == "" {
			return p, ""
		}
		p.Type = patches.TypeFileWriteAtomic
		return p, "declared search_replace but carries a whole file"
	}
	return p, ""
}

// normalizeFinalPatches fixes each patch's label and returns one line per
// patch it reinterpreted, for the log. Silence here would put the next reader
// of a trace in the position this investigation started from: a patch applied
// as something other than what it said, with nothing recording the difference.
func normalizeFinalPatches(ps []patches.Patch) ([]patches.Patch, []string) {
	if len(ps) == 0 {
		return ps, nil
	}
	out := make([]patches.Patch, 0, len(ps))
	var notes []string
	for _, p := range ps {
		fixed, why := normalizeFinalPatch(p)
		if why != "" {
			notes = append(notes, strings.TrimSpace(p.Path)+": "+why+
				" — applied as "+string(fixed.Type))
		}
		out = append(out, fixed)
	}
	return out, notes
}
