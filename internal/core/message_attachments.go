package core

import (
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/attachments"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

// MessageAttachment is the JSON-RPC attachment reference (alias for protocol docs).
type MessageAttachment = attachments.MessageAttachment

func attachmentsToUI(atts []MessageAttachment) []sessionfile.UIAttachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]sessionfile.UIAttachment, 0, len(atts))
	for _, a := range atts {
		p := strings.TrimSpace(a.Path)
		if p == "" {
			continue
		}
		name := strings.TrimSpace(a.Name)
		if name == "" {
			name = filepath.Base(p)
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		kind := attachments.ResolveKind(a)
		out = append(out, sessionfile.UIAttachment{
			Path: p,
			Name: name,
			Kind: kind,
			MIME: strings.TrimSpace(a.MIME),
			Ext:  ext,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildUserUIMessage(text string, atts []MessageAttachment) sessionfile.UIMessage {
	return sessionfile.UIMessage{
		Role:        "user",
		Text:        strings.TrimSpace(text),
		Attachments: attachmentsToUI(atts),
	}
}

func resolveTurnQuery(content string, atts []MessageAttachment, multimodal bool) string {
	q := attachments.MergeQueryWithFileRefs(content, atts)
	if multimodal {
		if hints := attachments.ImageNameHints(atts); hints != "" && strings.TrimSpace(content) == "" {
			if q == "" {
				return "Attached images: " + hints
			}
		}
	}
	return q
}

func loadAttachmentImages(cfg *config.ProjectConfig, workspaceRoot string, atts []MessageAttachment) ([]llm.ContentPart, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	if err := attachments.ValidatePaths(workspaceRoot, atts); err != nil {
		return nil, protocol.NewError(protocol.PathTraversal, err.Error(), nil)
	}
	hasImage := false
	for _, a := range atts {
		if attachments.ResolveKind(a) == "image" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return nil, nil
	}
	if cfg == nil || !cfg.LLM.Multimodal {
		return nil, protocol.NewError(protocol.InvalidParams,
			"image attachments require llm.multimodal: true with a vision-capable model", nil)
	}
	parts, err := attachments.LoadImageParts(atts)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return parts, nil
}

func validateTurnInput(content string, atts []MessageAttachment) error {
	if strings.TrimSpace(content) == "" && len(atts) == 0 {
		return protocol.NewError(protocol.InvalidLLMOutput, "content is empty", nil)
	}
	for _, a := range atts {
		if strings.TrimSpace(a.Path) == "" {
			return protocol.NewError(protocol.InvalidParams, "attachment path is empty", nil)
		}
	}
	return nil
}

func enrichQueryWithImageHints(query string, atts []MessageAttachment) string {
	q := strings.TrimSpace(query)
	if q != "" {
		return q
	}
	if hints := attachments.ImageNameHints(atts); hints != "" {
		return "Attached images: " + hints
	}
	return q
}
