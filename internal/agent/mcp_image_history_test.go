package agent

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// An MCP tool result carried its images as base64 inside the JSON the model
// reads. A text model got kilobytes of noise in its context on every later
// step (seen live: 5.5 KB for the MCP logo, and a Playwright MCP screenshot is
// hundreds), and a multimodal one got every image twice — once as this text,
// once as the real image message beside it. The history keeps a short note
// instead; the image message is built from the raw result, not from this.
func TestToolHistory_MCPImagesAreNotPastedAsBase64(t *testing.T) {
	png := base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 3000)...))
	out, _ := json.Marshal(map[string]any{
		"result": "Here's the image you requested",
		"images": []map[string]string{{"data": png, "mime": "image/png"}},
	})

	for _, multimodal := range []bool{false, true} {
		a := &Agent{opts: Options{MultimodalLLM: multimodal}}
		got := a.prepareToolHistoryContent("mcp:everything:get-tiny-image", nil, out)
		if strings.Contains(got, png[:40]) {
			t.Errorf("multimodal=%v: the image's base64 is in the history text (%d bytes)", multimodal, len(got))
		}
		if !strings.Contains(got, "Here's the image you requested") {
			t.Errorf("multimodal=%v: the tool's text was lost: %s", multimodal, got)
		}
		if !strings.Contains(got, "image/png") {
			t.Errorf("multimodal=%v: the note does not say an image came back: %s", multimodal, got)
		}
		shown := strings.Contains(got, "next message")
		if shown != multimodal {
			t.Errorf("multimodal=%v: the note says the image is shown = %v: %s", multimodal, shown, got)
		}
	}

	// Other tools' output is untouched.
	a := &Agent{}
	if got := a.prepareToolHistoryContent("read", nil, out); got != string(out) {
		t.Errorf("a non-MCP result was rewritten: %s", got)
	}
}
