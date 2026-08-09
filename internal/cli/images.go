package cli

import (
	"github.com/orchestra/orchestra/internal/attachments"
	"github.com/orchestra/orchestra/llm"
)

// loadImageParts reads the named files and returns a slice of PartImage
// ContentParts ready to attach to an llm.Message. Paths are resolved
// relative to the current working directory (the user-facing surface).
// Unsupported extensions return a clear error before the LLM is called.
func loadImageParts(paths []string) ([]llm.ContentPart, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	atts := make([]attachments.MessageAttachment, 0, len(paths))
	for _, p := range paths {
		atts = append(atts, attachments.MessageAttachment{Path: p, Kind: "image"})
	}
	parts, err := attachments.LoadImageParts(atts)
	if err != nil {
		// Preserve legacy CLI flag wording for tests/docs.
		if len(paths) == 1 {
			return nil, err
		}
		return nil, err
	}
	return parts, nil
}
