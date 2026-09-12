package core

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	attpkg "github.com/orchestra/orchestra/internal/attachments"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/protocol"
)

// AttachmentsStoreParams carries a file's bytes from a client that has no
// filesystem of its own — a browser, or the desktop shell's web view — to be
// kept under the workspace, where a turn can refer to it. The editor's host
// does this itself (ui/vscode/src/chat/panel.ts, "attachBytes"): this is the
// same thing for hosts whose only reach into the workspace is the core.
type AttachmentsStoreParams struct {
	Name       string `json:"name"`
	MIME       string `json:"mime,omitempty"`
	DataBase64 string `json:"data_base64"`
}

// AttachmentsStoreResult is the stored file as a message attachment: the
// same fields the renderer's file chips and session.message carry.
type AttachmentsStoreResult struct {
	Name string `json:"name"`
	// Path is absolute; Rel is workspace-relative with forward slashes.
	Path string `json:"path"`
	Rel  string `json:"rel"`
	Ext  string `json:"ext,omitempty"`
	Kind string `json:"kind"` // image | file
	Size int    `json:"size"`
}

// maxStoredAttachmentBytes matches MAX_ATTACHMENT_BYTES in the editor host
// and MAX_ATTACH_BYTES in the renderer: the same file is refused everywhere.
const maxStoredAttachmentBytes = 20 * 1024 * 1024

// AttachmentsStore writes the bytes to <workspace>/.orchestra/attachments/
// and answers with the attachment to send. Inside the workspace by
// construction, so the path passes the same check every other attachment
// does (attachments.ValidatePaths); the core never writes anywhere else.
func (c *Core) AttachmentsStore(params AttachmentsStoreParams) (*AttachmentsStoreResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(params.DataBase64))
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, "data_base64 is not base64: "+err.Error(), nil)
	}
	if len(data) == 0 {
		return nil, protocol.NewError(protocol.InvalidParams, "attachment is empty", nil)
	}
	if len(data) > maxStoredAttachmentBytes {
		return nil, protocol.NewError(protocol.InvalidParams,
			fmt.Sprintf("attachment exceeds %d MB", maxStoredAttachmentBytes/(1024*1024)), nil)
	}

	name := safeAttachmentName(params.Name, params.MIME)
	dest := filepath.Join(c.workspaceRoot, ".orchestra", "attachments",
		fmt.Sprintf("%d-%s", time.Now().UnixMilli(), name))
	abs, rel, err := fsutil.ResolveInWorkspace(c.workspaceRoot, dest)
	if err != nil {
		return nil, protocol.NewError(protocol.PathTraversal, err.Error(), nil)
	}
	if err := fsutil.AtomicWriteFile(abs, data, 0o644); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, "store attachment: "+err.Error(), nil)
	}
	return &AttachmentsStoreResult{
		Name: name,
		Path: abs,
		Rel:  rel,
		Ext:  strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."),
		Kind: attpkg.ResolveKind(attpkg.MessageAttachment{Path: abs, MIME: params.MIME}),
		Size: len(data),
	}, nil
}

var (
	attachmentNameUnsafe = regexp.MustCompile(`[^\w.\-()+ ]+`)
	attachmentNameSpaces = regexp.MustCompile(`\s+`)
)

// mimeExt names a pasted image or blob that arrived without an extension.
var mimeExt = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/bmp":       ".bmp",
	"image/svg+xml":   ".svg",
	"text/plain":      ".txt",
	"application/pdf": ".pdf",
}

// safeAttachmentName is the editor host's spelling of a stored name: the
// base name only, unsafe characters folded to "_", spaces to "-", and an
// extension from the MIME type when the name has none — a pasted screenshot
// is "image.png", not "image".
func safeAttachmentName(name, mime string) string {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == "/" || name == string(filepath.Separator) {
		name = "attachment"
	}
	name = attachmentNameUnsafe.ReplaceAllString(name, "_")
	name = attachmentNameSpaces.ReplaceAllString(name, "-")
	if !strings.Contains(name, ".") {
		ext := mimeExt[strings.ToLower(strings.TrimSpace(mime))]
		if ext == "" {
			ext = ".bin"
		}
		name += ext
	}
	return name
}
