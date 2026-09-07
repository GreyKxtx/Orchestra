package sessionfile

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// ExportHTML loads a session and renders it as one self-contained HTML file.
//
// The JSON bundle Export writes is for machines — session import reads it back.
// This is the readable counterpart: a single file to open in a browser or send
// to someone who does not have Orchestra.
func ExportHTML(workspaceRoot, id string) ([]byte, error) {
	if err := ValidateSessionID(id); err != nil {
		return nil, err
	}
	snap, err := Load(workspaceRoot, id)
	if err != nil {
		return nil, err
	}
	return RenderSessionHTML(snap), nil
}

// RenderSessionHTML renders a snapshot as a standalone HTML document.
//
// Self-contained means exactly that: inline CSS, no scripts, no network
// references. A page that fetches a stylesheet renders wrong offline and tells
// whoever hosts that stylesheet who opened the transcript. Collapsing sections
// use <details>, which needs no JavaScript.
//
// Every value from the session is escaped. Message text is arbitrary user and
// model output — a session that discussed HTML must not execute when opened.
func RenderSessionHTML(snap *Snapshot) []byte {
	var b strings.Builder
	if snap == nil {
		return []byte("<!doctype html><meta charset=\"utf-8\"><title>empty session</title>")
	}
	title := strings.TrimSpace(snap.Title)
	if title == "" {
		title = snap.ID
	}

	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title))
	b.WriteString(sessionHTMLStyle)
	b.WriteString("<body>\n<main>\n")

	// Header
	b.WriteString("<header>\n")
	fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(title))
	var meta []string
	meta = append(meta, "id "+html.EscapeString(snap.ID))
	if !snap.CreatedAt.IsZero() {
		meta = append(meta, html.EscapeString(snap.CreatedAt.Format(time.RFC3339)))
	}
	if !snap.UpdatedAt.IsZero() && !snap.UpdatedAt.Equal(snap.CreatedAt) {
		meta = append(meta, "→ "+html.EscapeString(snap.UpdatedAt.Format(time.RFC3339)))
	}
	if m := strings.TrimSpace(snap.Model); m != "" {
		meta = append(meta, html.EscapeString(m))
	}
	meta = append(meta, fmt.Sprintf("%d messages", len(snap.UIMessages)))
	if snap.CostUSD > 0 {
		meta = append(meta, fmt.Sprintf("$%.4f", snap.CostUSD))
	}
	if snap.ParentID != "" {
		meta = append(meta, "forked from "+html.EscapeString(snap.ParentID))
	}
	fmt.Fprintf(&b, "<p class=\"meta\">%s</p>\n", strings.Join(meta, " &middot; "))
	b.WriteString("</header>\n")

	for _, m := range snap.UIMessages {
		writeMessageHTML(&b, m)
	}

	b.WriteString("</main>\n")
	b.WriteString("<footer class=\"meta\">Exported by Orchestra &middot; this file is self-contained</footer>\n")
	b.WriteString("</body>\n</html>\n")
	return []byte(b.String())
}

func writeMessageHTML(b *strings.Builder, m UIMessage) {
	role := strings.TrimSpace(m.Role)
	if role == "" {
		role = "assistant"
	}
	fmt.Fprintf(b, "<article class=\"msg %s\">\n", html.EscapeString(roleClass(role)))
	fmt.Fprintf(b, "<div class=\"role\">%s</div>\n", html.EscapeString(role))

	// Segments are the newer persisted shape and carry their own ordering. A
	// message that has them is rendered from them alone; falling through to the
	// flat fields as well would print the same turn twice.
	if len(m.Segments) > 0 {
		for _, seg := range m.Segments {
			switch seg.Kind {
			case "reasoning":
				writeReasoningHTML(b, seg.Text)
			case "tools":
				writeToolsHTML(b, seg.Tools)
			default:
				writeTextHTML(b, seg.Text)
			}
		}
	} else {
		writeReasoningHTML(b, m.Reasoning)
		writeTextHTML(b, m.Text)
		writeToolsHTML(b, m.ToolBlocks)
	}

	for _, n := range m.Notices {
		if strings.TrimSpace(n.Text) == "" {
			continue
		}
		fmt.Fprintf(b, "<p class=\"notice %s\">%s</p>\n",
			html.EscapeString(roleClass(n.Kind)), html.EscapeString(n.Text))
	}
	for _, d := range m.DiffFiles {
		fmt.Fprintf(b, "<div class=\"diff-file\">%s</div>\n", html.EscapeString(d.Path))
	}

	var stats []string
	if m.DurationMS > 0 {
		stats = append(stats, formatDurationMS(m.DurationMS))
	}
	if m.TokensIn > 0 {
		stats = append(stats, fmt.Sprintf("%d in", m.TokensIn))
	}
	if m.TokensOut > 0 {
		stats = append(stats, fmt.Sprintf("%d out", m.TokensOut))
	}
	if len(stats) > 0 {
		fmt.Fprintf(b, "<p class=\"meta\">%s</p>\n", html.EscapeString(strings.Join(stats, " · ")))
	}
	b.WriteString("</article>\n")
}

func writeTextHTML(b *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// <pre> rather than markdown: rendering markdown means either a dependency
	// or a hand-rolled parser, and a transcript is more useful faithful than
	// prettified. Wrapping is handled in CSS.
	fmt.Fprintf(b, "<pre class=\"text\">%s</pre>\n", html.EscapeString(text))
}

func writeReasoningHTML(b *strings.Builder, reasoning string) {
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return
	}
	b.WriteString("<details class=\"reasoning\"><summary>reasoning</summary>\n")
	fmt.Fprintf(b, "<pre>%s</pre>\n", html.EscapeString(reasoning))
	b.WriteString("</details>\n")
}

func writeToolsHTML(b *strings.Builder, tools []UIToolBlock) {
	if len(tools) == 0 {
		return
	}
	b.WriteString("<div class=\"tools\">\n")
	for _, t := range tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = "tool"
		}
		head := html.EscapeString(name)
		if args := strings.TrimSpace(firstNonEmptyStr(t.ArgsPreview, t.ArgsRaw)); args != "" {
			head += " <span class=\"args\">" + html.EscapeString(truncateRunes(args, 120)) + "</span>"
		}
		if t.DurationMS > 0 {
			head += " <span class=\"dur\">" + html.EscapeString(formatDurationMS(t.DurationMS)) + "</span>"
		}
		if st := strings.TrimSpace(t.Status); st != "" && st != "completed" {
			head += " <span class=\"status\">" + html.EscapeString(st) + "</span>"
		}
		b.WriteString("<details class=\"tool\"><summary>" + head + "</summary>\n")
		if res := strings.TrimSpace(t.Result); res != "" {
			fmt.Fprintf(b, "<pre>%s</pre>\n", html.EscapeString(truncateRunes(res, 8000)))
		}
		for _, d := range t.Diagnostics {
			fmt.Fprintf(b, "<p class=\"diag %s\">L%d: %s</p>\n",
				html.EscapeString(roleClass(d.Severity)), d.StartLine, html.EscapeString(d.Message))
		}
		b.WriteString("</details>\n")
	}
	b.WriteString("</div>\n")
}

// roleClass keeps arbitrary persisted strings out of the class attribute: a
// role or severity is data, and data does not get to name a CSS class or, with
// a well-chosen quote, end the attribute.
func roleClass(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, allowed := range []string{
		"user", "assistant", "system", "tool",
		"info", "error", "warning", "retry", "success", "hint",
	} {
		if s == allowed {
			return s
		}
	}
	return "other"
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// formatDurationMS matches the TUI's tool group and the VS Code webview:
// sub-second in ms, then one decimal of seconds, then m/s past a minute.
func formatDurationMS(ms int64) string {
	switch {
	case ms <= 0:
		return ""
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 59950:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	default:
		totalSec := (ms + 500) / 1000
		return fmt.Sprintf("%dm %02ds", totalSec/60, totalSec%60)
	}
}

// sessionHTMLStyle is the whole stylesheet, inline. Both colour schemes are
// defined so the file reads on either background — the reader's browser, not
// the exporter's, decides.
const sessionHTMLStyle = `<style>
:root {
  --bg: #ffffff; --fg: #1c1e21; --muted: #6b7280; --line: #e5e7eb;
  --user-bg: #eef2ff; --tool-bg: #f6f7f9; --accent: #4f46e5;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #16181d; --fg: #e6e6e6; --muted: #9aa0a6; --line: #2c2f36;
    --user-bg: #1e2333; --tool-bg: #1c1f25; --accent: #8b9bff;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0; padding: 24px 16px; background: var(--bg); color: var(--fg);
  font: 14px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}
main { max-width: 860px; margin: 0 auto; }
header { border-bottom: 1px solid var(--line); padding-bottom: 12px; margin-bottom: 20px; }
h1 { font-size: 20px; margin: 0 0 4px; }
.meta { color: var(--muted); font-size: 12px; margin: 4px 0; }
.msg { border-bottom: 1px solid var(--line); padding: 14px 0; }
.msg:last-of-type { border-bottom: 0; }
.role {
  font-size: 11px; text-transform: uppercase; letter-spacing: .06em;
  color: var(--muted); margin-bottom: 6px;
}
.msg.user { background: var(--user-bg); border-radius: 8px; padding: 12px 14px; border-bottom: 0; }
.msg.user .role { color: var(--accent); }
pre.text, details pre {
  margin: 0; white-space: pre-wrap; overflow-wrap: anywhere;
  font: 13px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
details { margin: 8px 0; }
details pre {
  background: var(--tool-bg); border: 1px solid var(--line); border-radius: 6px;
  padding: 8px 10px; max-height: 420px; overflow: auto;
}
summary { cursor: pointer; color: var(--muted); font-size: 12px; }
.tool summary { color: var(--fg); }
.args { color: var(--muted); font-family: ui-monospace, monospace; }
.dur { color: var(--muted); font-variant-numeric: tabular-nums; }
.status { color: #d97706; }
.diag { font-size: 12px; color: #d97706; margin: 4px 0 0; }
.diag.error { color: #dc2626; }
.notice { font-size: 12px; color: var(--muted); }
.notice.error { color: #dc2626; }
.diff-file { font-family: ui-monospace, monospace; font-size: 12px; color: var(--muted); }
footer { max-width: 860px; margin: 24px auto 0; border-top: 1px solid var(--line); padding-top: 10px; }
</style>
`
