package wire

// attachments.store.

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
