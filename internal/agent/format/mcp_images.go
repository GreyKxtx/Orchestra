package format

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

// maxMCPImagesPerCall caps how many images one MCP tool call may put into the
// conversation. A server that returns a gallery would otherwise spend the
// whole context window in a single call, and the model rarely needs more than
// a few frames to answer.
const maxMCPImagesPerCall = 4

// ReplaceMCPImagesWithNote rewrites an mcp:* tool result for the history text:
// each image's base64 is replaced by one line naming its type and size, and
// whether the model is shown it (in the message after the tool result). The
// text of the result is kept as it came. A result without images, or one that
// is not the manager's JSON, comes back unchanged.
func ReplaceMCPImagesWithNote(out []byte, shown bool) []byte {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(out, &resp); err != nil {
		return out
	}
	var images []struct {
		Data string `json:"data"`
		MIME string `json:"mime"`
	}
	if err := json.Unmarshal(resp["images"], &images); err != nil || len(images) == 0 {
		return out
	}
	notes := make([]string, 0, len(images))
	for i, img := range images {
		mime := strings.TrimSpace(img.MIME)
		if mime == "" {
			mime = "image/png"
		}
		size := base64.StdEncoding.DecodedLen(len(strings.TrimSpace(img.Data)))
		state := "not shown: this model is not given images"
		if shown {
			state = "shown to you in the next message"
			if i >= maxMCPImagesPerCall {
				state = "not shown: over the per-call image limit"
			}
		}
		notes = append(notes, fmt.Sprintf("image %d (%s, %.1f KB) — %s", i+1, mime, float64(size)/1024, state))
	}
	resp["images"], _ = json.Marshal(notes)
	rewritten, err := json.Marshal(resp)
	if err != nil {
		return out
	}
	return rewritten
}

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
