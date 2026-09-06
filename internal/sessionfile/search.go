package sessionfile

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

// snippetWidth caps how much of a matching message is shown, in runes.
const snippetWidth = 120

// Hit is one matching message inside one session.
type Hit struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	// Index is the position in ui_messages — the same index session.fork and
	// session.rewind take, so a search result can be acted on directly.
	Index   int    `json:"index"`
	Role    string `json:"role"`
	Snippet string `json:"snippet"`
}

// SearchOptions configures a session content search.
type SearchOptions struct {
	Query string
	// Insensitive mirrors `orchestra search -i`; the default is case-sensitive
	// so two sibling commands do not disagree.
	Insensitive bool
	// IncludeAll also searches reasoning and tool blocks, not just message text.
	IncludeAll bool
	// Limit caps the number of hits returned; 0 means no cap.
	Limit int
}

// Search scans every session in the project for messages containing the query.
//
// It parses each session file, exactly as ListMeta already does for the picker,
// so this adds work on bytes that were being read anyway rather than a new
// class of I/O. Files that cannot be read or parsed are skipped: one corrupt
// session must not make search fail for all the others.
func Search(workspaceRoot string, opts SearchOptions) ([]Hit, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, errors.New("search: query is empty")
	}
	entries, err := os.ReadDir(sessionsDir(workspaceRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	needle := opts.Query
	if opts.Insensitive {
		needle = strings.ToLower(needle)
	}

	type group struct {
		updatedAt time.Time
		hits      []Hit
	}
	var groups []group

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		snap, err := Load(workspaceRoot, strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		var hits []Hit
		for i, m := range snap.UIMessages {
			field, ok := matchField(m, needle, opts)
			if !ok {
				continue
			}
			hits = append(hits, Hit{
				SessionID: snap.ID,
				Title:     snap.Title,
				UpdatedAt: snap.UpdatedAt,
				Index:     i,
				Role:      m.Role,
				Snippet:   snippetAround(field, needle, opts.Insensitive),
			})
		}
		if len(hits) > 0 {
			groups = append(groups, group{updatedAt: snap.UpdatedAt, hits: hits})
		}
	}

	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].updatedAt.After(groups[j].updatedAt)
	})

	var out []Hit
	for _, g := range groups {
		for _, h := range g.hits {
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out, nil
			}
			out = append(out, h)
		}
	}
	return out, nil
}

// matchField returns the first field of m that contains needle, so the caller
// can build a snippet from it. Only one field is reported per message: a tool
// result mentioning the query forty times must not become forty rows.
func matchField(m UIMessage, needle string, opts SearchOptions) (string, bool) {
	fields := []string{m.Text}
	if opts.IncludeAll {
		fields = append(fields, m.Reasoning)
		for _, tb := range m.ToolBlocks {
			fields = append(fields, tb.Name, tb.ArgsPreview, tb.ArgsRaw, tb.Result)
		}
		for _, seg := range m.Segments {
			fields = append(fields, seg.Text)
			for _, tb := range seg.Tools {
				fields = append(fields, tb.Name, tb.ArgsPreview, tb.ArgsRaw, tb.Result)
			}
		}
	}
	for _, f := range fields {
		if f == "" {
			continue
		}
		hay := f
		if opts.Insensitive {
			hay = strings.ToLower(f)
		}
		if strings.Contains(hay, needle) {
			return f, true
		}
	}
	return "", false
}

// snippetAround renders a single-line window of text centred on the first
// occurrence of needle, capped at snippetWidth runes with … marking each cut end.
func snippetAround(text, needle string, insensitive bool) string {
	flat := strings.Join(strings.Fields(text), " ")
	runes := []rune(flat)
	if len(runes) <= snippetWidth {
		return flat
	}

	hay := flat
	if insensitive {
		hay = strings.ToLower(flat)
	}
	// Case folding can change byte length for a few runes, so the offset is
	// clamped before it is used to slice.
	byteAt := strings.Index(hay, needle)
	if byteAt < 0 || byteAt > len(flat) {
		byteAt = 0
	}
	matchAt := len([]rune(flat[:byteAt]))

	start := matchAt - snippetWidth/3
	if start < 0 {
		start = 0
	}
	end := start + snippetWidth
	if end > len(runes) {
		end = len(runes)
		start = end - snippetWidth
	}

	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
