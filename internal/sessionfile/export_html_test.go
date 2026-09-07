package sessionfile

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func htmlFixture(t *testing.T) *Snapshot {
	t.Helper()
	return &Snapshot{
		Version:   Version,
		ID:        "20260901T100000-aaaa",
		Title:     "wiring auth",
		Model:     "claude-opus-5",
		CreatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 1, 11, 30, 0, 0, time.UTC),
		UIMessages: []UIMessage{
			{Role: "user", Text: "how do I wire the bearer token"},
			{
				Role:      "assistant",
				Text:      "authTransport sets the header",
				Reasoning: "the transport is the only shared seam",
				ToolBlocks: []UIToolBlock{{
					Name:       "read",
					ArgsRaw:    `{"path":"internal/mcp/remote.go"}`,
					Status:     "completed",
					Result:     "package mcp",
					DurationMS: 1240,
				}},
			},
		},
		MsgCount: 2,
	}
}

// The point of the HTML export is a file someone can open or send. A page that
// pulls a stylesheet or script off the network is neither: it renders wrong
// offline and leaks a request to whoever is hosting it.
func TestRenderSessionHTML_IsSelfContained(t *testing.T) {
	out := string(RenderSessionHTML(htmlFixture(t)))

	external := regexp.MustCompile(`(?i)(src|href)\s*=\s*["']?(https?:)?//`)
	if m := external.FindString(out); m != "" {
		t.Errorf("export references an external resource (%q) — it is not self-contained", m)
	}
	for _, bad := range []string{"<script", "cdn.", "fonts.googleapis"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Errorf("export contains %q; the page must be inert markup plus inline CSS", bad)
		}
	}
	if !strings.Contains(out, "<style") {
		t.Error("export has no inline stylesheet — it would render as unstyled text")
	}
}

// Session text is arbitrary user and model output. A session that discussed
// HTML must not execute when the export is opened.
func TestRenderSessionHTML_EscapesContent(t *testing.T) {
	snap := htmlFixture(t)
	snap.Title = `t<itle> & "quotes"`
	snap.UIMessages = []UIMessage{
		{Role: "user", Text: `<script>alert('xss')</script>`},
		{Role: "assistant", Text: "a & b < c", Reasoning: `<img src=x onerror=alert(1)>`,
			ToolBlocks: []UIToolBlock{{
				Name:    `<b>read</b>`,
				ArgsRaw: `{"path":"<script>"}`,
				Status:  "completed",
				Result:  `</pre><script>alert(2)</script>`,
			}}},
	}
	out := string(RenderSessionHTML(snap))

	for _, raw := range []string{
		"<script>alert('xss')</script>",
		"<img src=x onerror=alert(1)>",
		"<script>alert(2)</script>",
		"<b>read</b>",
	} {
		if strings.Contains(out, raw) {
			t.Errorf("unescaped %q survived into the export — opening it would run the page's content", raw)
		}
	}
	// The text must still be there, escaped.
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("escaped script text is missing entirely; content was dropped rather than escaped")
	}
	if !strings.Contains(out, "a &amp; b &lt; c") {
		t.Error("ordinary text with & and < was not escaped correctly")
	}
}

func TestRenderSessionHTML_RendersTheConversation(t *testing.T) {
	out := string(RenderSessionHTML(htmlFixture(t)))

	for _, want := range []string{
		"wiring auth",                     // title
		"claude-opus-5",                   // model
		"how do I wire the bearer token",  // user text
		"authTransport sets the header",   // assistant text
		"the transport is the only shared seam", // reasoning
		"read",                            // tool name
		"internal/mcp/remote.go",          // tool args
		"package mcp",                     // tool result
	} {
		if !strings.Contains(out, want) {
			t.Errorf("export is missing %q", want)
		}
	}
	// duration_ms is persisted; an export that drops it throws away the one
	// number that says what the turn cost.
	if !strings.Contains(out, "1.2s") {
		t.Error("tool duration is missing from the export")
	}
}

// Segments are the newer persisted shape; a session written with segments and
// no top-level tool_blocks must not export as an empty assistant turn.
func TestRenderSessionHTML_ReadsSegments(t *testing.T) {
	snap := htmlFixture(t)
	snap.UIMessages = []UIMessage{
		{Role: "user", Text: "go on"},
		{Role: "assistant", Segments: []UISegment{
			{Kind: "reasoning", Text: "thinking about it"},
			{Kind: "text", Text: "here is the answer"},
			{Kind: "tools", Tools: []UIToolBlock{{Name: "bash", Status: "completed", Result: "ok"}}},
		}},
	}
	out := string(RenderSessionHTML(snap))
	for _, want := range []string{"thinking about it", "here is the answer", "bash"} {
		if !strings.Contains(out, want) {
			t.Errorf("segment content %q is missing from the export", want)
		}
	}
}

func TestExportHTML_LoadsFromDisk(t *testing.T) {
	root := t.TempDir()
	snap := htmlFixture(t)
	if err := Save(root, snap); err != nil {
		t.Fatal(err)
	}
	out, err := ExportHTML(root, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "how do I wire the bearer token") {
		t.Error("ExportHTML did not render the session it loaded")
	}
}

func TestExportHTML_RejectsATraversingID(t *testing.T) {
	if _, err := ExportHTML(t.TempDir(), "../../etc/passwd"); err == nil {
		t.Fatal("a traversing session id was accepted")
	}
}
