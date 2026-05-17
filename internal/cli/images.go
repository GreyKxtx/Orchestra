package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/llm"
)

// imageMimeByExt maps file extensions to the MIME types supported by
// OpenAI-compatible multimodal endpoints (and most VL models).
var imageMimeByExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// loadImageParts reads the named files and returns a slice of PartImage
// ContentParts ready to attach to an llm.Message. Paths are resolved
// relative to the current working directory (the user-facing surface).
// Unsupported extensions return a clear error before the LLM is called.
func loadImageParts(paths []string) ([]llm.ContentPart, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]llm.ContentPart, 0, len(paths))
	for _, p := range paths {
		mime, ok := imageMimeByExt[strings.ToLower(filepath.Ext(p))]
		if !ok {
			return nil, fmt.Errorf("--image %s: unsupported extension (supported: .png .jpg .jpeg .gif .webp)", p)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("--image %s: %w", p, err)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("--image %s: empty file", p)
		}
		out = append(out, llm.ContentPart{
			Kind:      llm.PartImage,
			ImageData: data,
			ImageMIME: mime,
		})
	}
	return out, nil
}
