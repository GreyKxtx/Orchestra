package agent

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/tools/toolpath"
	"github.com/orchestra/orchestra/patch/applier"
)

// With Apply, fs.delete and fs.rename act on the disk at once, outside the
// staging overlay that write and edit go through. The turn's result learned
// about the edits and not about these: a model that wrote a helper, ran it and
// deleted it was reported as having changed the helper, and a real deletion
// was not reported at all.

// diskMove is one delete or rename, with the file as it was before it. Paths
// are the workspace-relative form every other change of the turn is keyed by.
type diskMove struct {
	from, to       string // to is empty for a delete
	fromAbs, toAbs string
	before         string
	isDir          bool
}

// beforeDiskMove reads what a delete or a rename is about to move, when the
// turn applies; nil for any other call.
func (a *Agent) beforeDiskMove(name string, input json.RawMessage) *diskMove {
	if !a.opts.Apply || a.tools == nil || (name != "fs.delete" && name != "fs.rename") {
		return nil
	}
	var req struct {
		Path    string `json:"path"`
		NewPath string `json:"new_path"`
	}
	if json.Unmarshal(input, &req) != nil {
		return nil
	}
	root := a.tools.WorkspaceRoot()
	fromAbs, from, err := toolpath.ResolveWorkspacePath(root, strings.TrimSpace(req.Path))
	if err != nil || from == "" {
		return nil
	}
	m := &diskMove{from: from, fromAbs: fromAbs}
	if name == "fs.rename" {
		toAbs, to, err := toolpath.ResolveWorkspacePath(root, strings.TrimSpace(req.NewPath))
		if err != nil || to == "" {
			return nil
		}
		m.to, m.toAbs = to, toAbs
	}
	info, err := os.Stat(fromAbs)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		m.isDir = true
		return m
	}
	b, err := os.ReadFile(fromAbs)
	if err != nil {
		return nil
	}
	m.before = string(b)
	return m
}

// afterDiskMove adds a delete or rename that happened to the turn's committed
// changes: the file at from is gone, and for a rename it is at to. What
// happened is what the disk says — a delete that only previewed (a layer that
// does not commit to disk) answers without an error and moves nothing.
func (a *Agent) afterDiskMove(m *diskMove) {
	if m == nil {
		return
	}
	if _, err := os.Stat(m.fromAbs); err == nil {
		return // still there
	}
	if m.to != "" {
		if _, err := os.Stat(m.toAbs); err != nil {
			return // not arrived
		}
	}
	resp := &tools.FSApplyOpsResponse{Applied: true, ChangedFiles: []string{m.from}}
	if !m.isDir {
		resp.Diffs = append(resp.Diffs, applier.FileDiff{Path: m.from, Before: m.before, After: ""})
	}
	if m.to != "" {
		resp.ChangedFiles = append(resp.ChangedFiles, m.to)
		if !m.isDir {
			resp.Diffs = append(resp.Diffs, applier.FileDiff{Path: m.to, Before: "", After: m.before})
		}
	}
	a.turnCommitted = mergeApplyResponses(a.turnCommitted, resp)
}
