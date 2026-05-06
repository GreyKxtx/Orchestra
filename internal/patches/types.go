package patches

type Type string

const (
	TypeFileSearchReplace Type = "file.search_replace"
	TypeFileUnifiedDiff   Type = "file.unified_diff"
	TypeFileWriteAtomic   Type = "file.write_atomic"
)

// PatchSet is the vNext "External Patch" envelope.
type PatchSet struct {
	Patches []Patch `json:"patches"`
}

// Patch is a union of supported external patch kinds.
//
// Schema validation is expected to run before decoding.
type Patch struct {
	Type Type   `json:"type"`
	Path string `json:"path"`

	// file.search_replace
	Search  string `json:"search,omitempty"`
	Replace string `json:"replace,omitempty"`

	// file.unified_diff
	Diff string `json:"diff,omitempty"`

	// file.write_atomic
	Content    string               `json:"content,omitempty"`
	Mode       int                  `json:"mode,omitempty"` // e.g. 420 = 0644
	Conditions *WriteAtomicConditions `json:"conditions,omitempty"`

	// Versioning (minimum vNext): sha256:<hex> of file content used to plan the patch.
	// Required for search_replace/unified_diff. For write_atomic, prefer conditions.file_hash.
	FileHash string `json:"file_hash,omitempty"`
}

type WriteAtomicConditions struct {
	MustNotExist bool   `json:"must_not_exist,omitempty"`
	FileHash     string `json:"file_hash,omitempty"` // sha256:<hex>
}
