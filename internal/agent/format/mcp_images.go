package format

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

// maxMCPImagesPerCall caps how many images one MCP tool call may put into the
// conversation. A server that returns a gallery would otherwise spend the
// whole context window in a single call, and the model rarely needs more than
// a few frames to answer.
const maxMCPImagesPerCall = 4

// ExtractMCPImageParts pulls the images out of an mcp:* tool result so they
// can be shown to the model as real image content rather than described in
// text. Returns nil for every other tool and for any result without usable
// images — a malformed or undecodable image is a miss, never a guess.
func ExtractMCPImageParts(out json.RawMessage) []llm.ContentPart {
	var resp struct {
		Images []struct {
			Data string `json:"data"`
			MIME string `json:"mime"`
		} `json:"images"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || len(resp.Images) == 0 {
		return nil
	}
	var parts []llm.ContentPart
	for _, img := range resp.Images {
		if len(parts) >= maxMCPImagesPerCall {
			break
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(img.Data))
		if err != nil || len(data) == 0 {
			continue
		}
		mime := strings.TrimSpace(img.MIME)
		if mime == "" {
			mime = "image/png"
		}
		parts = append(parts, llm.ContentPart{Kind: llm.PartImage, ImageData: data, ImageMIME: mime})
	}
	return parts
}
