package attachments

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

const MaxImageBytes = 20 * 1024 * 1024

// MessageAttachment is a chat attachment referenced over JSON-RPC.
type MessageAttachment struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"` // image | file
	MIME string `json:"mime,omitempty"`
	Name string `json:"name,omitempty"`
}

var imageMimeByExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".avif": "image/avif",
	".svg":  "image/svg+xml",
}

// fileMimeByExt covers non-image attachments referenced via @path.
var fileMimeByExt = map[string]string{
	".pdf": "application/pdf",
}

// IsImagePath reports whether the extension looks like a supported image.
func IsImagePath(p string) bool {
	_, ok := imageMimeByExt[strings.ToLower(filepath.Ext(p))]
	return ok
}

// MIMEForPath returns MIME for known image or document extensions.
func MIMEForPath(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	if m, ok := imageMimeByExt[ext]; ok {
		return m
	}
	if m, ok := fileMimeByExt[ext]; ok {
		return m
	}
	return ""
}

// ResolveKind returns image or file for an attachment with optional kind hint.
func ResolveKind(a MessageAttachment) string {
	k := strings.ToLower(strings.TrimSpace(a.Kind))
	if k == "image" || k == "file" {
		return k
	}
	if IsImagePath(a.Path) {
		return "image"
	}
	return "file"
}

// LoadImageParts reads image attachments into LLM content parts.
// Paths must already be validated under workspaceRoot when workspaceRoot is non-empty.
func LoadImageParts(atts []MessageAttachment) ([]llm.ContentPart, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	out := make([]llm.ContentPart, 0, len(atts))
	for _, a := range atts {
		if ResolveKind(a) != "image" {
			continue
		}
		p := strings.TrimSpace(a.Path)
		if p == "" {
			continue
		}
		mime := strings.TrimSpace(a.MIME)
		if mime == "" {
			mime = MIMEForPath(p)
		}
		if mime == "" {
			return nil, fmt.Errorf("attachment %s: unsupported image extension", p)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", p, err)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("attachment %s: empty file", p)
		}
		if len(data) > MaxImageBytes {
			return nil, fmt.Errorf("attachment %s: exceeds %d MB limit", p, MaxImageBytes/(1024*1024))
		}
		out = append(out, llm.ContentPart{
			Kind:      llm.PartImage,
			ImageData: data,
			ImageMIME: mime,
		})
	}
	return out, nil
}

// MergeQueryWithFileRefs appends @path mentions for non-image attachments.
func MergeQueryWithFileRefs(content string, atts []MessageAttachment) string {
	q := strings.TrimSpace(content)
	var refs []string
	for _, a := range atts {
		if ResolveKind(a) != "file" {
			continue
		}
		p := strings.TrimSpace(a.Path)
		if p == "" {
			continue
		}
		refs = append(refs, "@"+p)
	}
	if len(refs) == 0 {
		return q
	}
	block := strings.Join(refs, " ")
	if q == "" {
		return block
	}
	return q + "\n\n" + block
}

// ImageNameHints returns short filenames for multimodal turns (tool hints).
func ImageNameHints(atts []MessageAttachment) string {
	var names []string
	for _, a := range atts {
		if ResolveKind(a) != "image" {
			continue
		}
		name := strings.TrimSpace(a.Name)
		if name == "" {
			name = filepath.Base(a.Path)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}
