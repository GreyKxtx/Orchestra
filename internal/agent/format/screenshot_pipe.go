package format

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/internal/llm"
)

// extractScreenshotImagePart parses a browser.screenshot tool response
// JSON and returns the embedded image as a PartImage suitable for
// injecting into chat history. Returns (zero, false) when the response
// is malformed, has no image, or the base64 is invalid.
//
// The tool response shape is {"image": "<base64 PNG>"} or
// {"image": "data:image/png;base64,..."}. Both are accepted.
func ExtractScreenshotImagePart(out json.RawMessage) (llm.ContentPart, bool) {
	var resp struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return llm.ContentPart{}, false
	}
	img := strings.TrimSpace(resp.Image)
	if img == "" {
		return llm.ContentPart{}, false
	}
	// Already a data URI вЂ” pass through verbatim.
	if strings.HasPrefix(img, "data:image/") {
		return llm.ContentPart{Kind: llm.PartImage, ImageURL: img}, true
	}
	// Raw base64 вЂ” decode to bytes and let the LLM client emit the data URI.
	data, err := base64.StdEncoding.DecodeString(img)
	if err != nil {
		return llm.ContentPart{}, false
	}
	if len(data) == 0 {
		return llm.ContentPart{}, false
	}
	return llm.ContentPart{Kind: llm.PartImage, ImageData: data, ImageMIME: "image/png"}, true
}
