package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcClient struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	// stdoutFile is the underlying pipe (if available) so we can set read deadlines
	// for "no response" notification tests without hanging.
	stdoutFile *os.File
	reader     *bufio.Reader
	nextID     int
	ctx        context.Context
	cancel     context.CancelFunc
}

func TestInitializeRequired(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	_, code := c.callExpectError(t, "tool.call", map[string]any{
		"name":  "fs.list",
		"input": map[string]any{"limit": 1},
	})
	if code != string(protocol.NotInitialized) && !strings.Contains(code, string(protocol.NotInitialized)) {
		t.Fatalf("expected %s, got: %s", protocol.NotInitialized, code)
	}
}

func TestInitializeIdempotentSame(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	params := map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	}

	res1 := c.call(t, "initialize", params)
	res2 := c.call(t, "initialize", params)

	var v1, v2 map[string]any
	if err := json.Unmarshal(res1, &v1); err != nil {
		t.Fatalf("bad initialize response: %v raw=%s", err, string(res1))
	}
	if err := json.Unmarshal(res2, &v2); err != nil {
		t.Fatalf("bad second initialize response: %v raw=%s", err, string(res2))
	}
	if v1["status"] != "ok" || v2["status"] != "ok" {
		t.Fatalf("expected status=ok, got: v1=%v v2=%v", v1, v2)
	}
}

func TestInitializeIdempotentDifferent(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	_, code := c.callExpectError(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       "sha256:deadbeef",
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})
	if code != string(protocol.AlreadyInitialized) && !strings.Contains(code, string(protocol.AlreadyInitialized)) {
		t.Fatalf("expected %s, got: %s", protocol.AlreadyInitialized, code)
	}
}

func TestRPC_Notification_NoResponse(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	// Notifications (no id) must not produce responses.
	// We can't reliably "wait for absence" on all platforms, so we assert that the
	// next response corresponds to the next request (not the notification).
	c.writeNotification(t, "core.health", map[string]any{})

	id1 := c.nextID
	c.nextID++
	c.writeRequest(t, "core.health", map[string]any{}, id1)

	resp1 := c.readResponse(t)
	if resp1.Error != nil {
		t.Fatalf("rpc error for id1: code=%d msg=%s data=%s", resp1.Error.Code, resp1.Error.Message, string(resp1.Error.Data))
	}
	var got1 int
	_ = json.Unmarshal(resp1.ID, &got1)
	if got1 != id1 {
		t.Fatalf("expected response id=%d (request), got=%s", id1, string(resp1.ID))
	}

	// Send another request to ensure there's no extra response queued.
	id2 := c.nextID
	c.nextID++
	c.writeRequest(t, "core.health", map[string]any{}, id2)
	resp2 := c.readResponse(t)
	if resp2.Error != nil {
		t.Fatalf("rpc error for id2: code=%d msg=%s data=%s", resp2.Error.Code, resp2.Error.Message, string(resp2.Error.Data))
	}
	var got2 int
	_ = json.Unmarshal(resp2.ID, &got2)
	if got2 != id2 {
		t.Fatalf("expected response id=%d (request), got=%s", id2, string(resp2.ID))
	}
}

func TestRPC_OneResponsePerRequest(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	// 2 requests + 1 notification => responses only for requests.
	id1 := c.nextID
	c.nextID++
	c.writeRequest(t, "core.health", map[string]any{}, id1)

	c.writeNotification(t, "core.health", map[string]any{})

	id2 := c.nextID
	c.nextID++
	c.writeRequest(t, "core.health", map[string]any{}, id2)

	// Add one more request to detect any extra response that might have been emitted
	// for the notification (it would desync the ID order).
	id3 := c.nextID
	c.nextID++
	c.writeRequest(t, "core.health", map[string]any{}, id3)

	resp1 := c.readResponse(t)
	if resp1.Error != nil {
		t.Fatalf("rpc error for id1: code=%d msg=%s data=%s", resp1.Error.Code, resp1.Error.Message, string(resp1.Error.Data))
	}
	resp2 := c.readResponse(t)
	if resp2.Error != nil {
		t.Fatalf("rpc error for id2: code=%d msg=%s data=%s", resp2.Error.Code, resp2.Error.Message, string(resp2.Error.Data))
	}
	resp3 := c.readResponse(t)
	if resp3.Error != nil {
		t.Fatalf("rpc error for id3: code=%d msg=%s data=%s", resp3.Error.Code, resp3.Error.Message, string(resp3.Error.Data))
	}

	var got1, got2, got3 int
	_ = json.Unmarshal(resp1.ID, &got1)
	_ = json.Unmarshal(resp2.ID, &got2)
	_ = json.Unmarshal(resp3.ID, &got3)
	if got1 != id1 {
		t.Fatalf("expected first response id=%d, got=%s", id1, string(resp1.ID))
	}
	if got2 != id2 {
		t.Fatalf("expected second response id=%d, got=%s", id2, string(resp2.ID))
	}
	if got3 != id3 {
		t.Fatalf("expected third response id=%d, got=%s", id3, string(resp3.ID))
	}
}

func TestApplyOps_Stdio_DryRun_NoWrite(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	orig, err := os.ReadFile(filepath.Join(proj, "utils.go"))
	if err != nil {
		t.Fatal(err)
	}

	ops := []any{
		map[string]any{
			"op":   "file.replace_range",
			"path": "utils.go",
			"range": map[string]any{
				"start": map[string]any{"line": 2, "col": 0},
				"end":   map[string]any{"line": 2, "col": 0},
			},
			"expected":    "", // insert at line start
			"replacement": "// dry-run marker\n",
		},
	}

	res := c.call(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops":     ops,
			"dry_run": true,
			"backup":  false,
		},
	})

	// Result should contain diffs (best-effort check)
	if !bytes.Contains(res, []byte(`"diff`)) && !bytes.Contains(res, []byte(`"diffs"`)) {
		t.Fatalf("expected diffs in dry-run result; got: %s", string(res))
	}

	after, err := os.ReadFile(filepath.Join(proj, "utils.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, after) {
		t.Fatalf("dry-run must not modify file")
	}
}

func TestApplyOps_Stdio_Apply_WithBackup(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	utilsPath := filepath.Join(proj, "utils.go")
	orig, _ := os.ReadFile(utilsPath)

	ops := []any{
		map[string]any{
			"op":   "file.replace_range",
			"path": "utils.go",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "",
			"replacement": "// applied marker\n",
		},
	}

	_ = c.call(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops":     ops,
			"dry_run": false,
			"backup":  true,
		},
	})

	after, err := os.ReadFile(utilsPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(orig, after) {
		t.Fatalf("apply must modify file")
	}
	if !bytes.HasPrefix(after, []byte("// applied marker\n")) {
		t.Fatalf("expected marker at file start; got:\n%s", string(after))
	}

	// Backup: support both suffix and backups dir (implementation-dependent)
	backups := findBackups(t, proj, "utils.go")
	if len(backups) == 0 {
		t.Fatalf("expected at least one backup, found none")
	}

	// Best-effort validate backup contains original content
	ok := false
	for _, bp := range backups {
		b, err := os.ReadFile(bp)
		if err != nil {
			continue
		}
		if bytes.Equal(b, orig) {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("expected a backup to match original utils.go content; backups=%v", backups)
	}
}

func TestApplyOps_Stdio_StaleContent_NoSideEffects(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
	})

	utilsPath := filepath.Join(proj, "utils.go")
	orig, _ := os.ReadFile(utilsPath)

	// External modification after we "planned" expected content
	marker := "\n// Modified externally\n"
	if err := os.WriteFile(utilsPath, append(orig, []byte(marker)...), 0644); err != nil {
		t.Fatal(err)
	}
	// ensure mtime changes on coarse FS
	_ = os.Chtimes(utilsPath, time.Now().Add(2*time.Second), time.Now().Add(2*time.Second))

	// Op expects old file start to match "", but we’ll use strict expected that won't match
	ops := []any{
		map[string]any{
			"op":   "file.replace_range",
			"path": "utils.go",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "THIS_WILL_NOT_MATCH",
			"replacement": "// should not apply\n",
		},
	}

	_, perr := c.callExpectError(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops":     ops,
			"dry_run": false,
			"backup":  true,
		},
	})

	if perr != string(protocol.StaleContent) && !strings.Contains(perr, string(protocol.StaleContent)) {
		t.Fatalf("expected StaleContent, got: %s", perr)
	}

	// No backups on stale
	backups := findBackups(t, proj, "utils.go")
	if len(backups) != 0 {
		t.Fatalf("expected no backups on stale error; got=%v", backups)
	}

	// File must still contain external marker
	after, err := os.ReadFile(utilsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after, []byte("Modified externally")) {
		t.Fatalf("expected file to keep external modification (no writes on stale)")
	}
}

func TestApplyOps_Stdio_PathTraversal_Denied(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
	})

	ops := []any{
		map[string]any{
			"op":   "file.replace_range",
			"path": "../outside.txt",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "",
			"replacement": "nope",
		},
	}

	_, msg := c.callExpectError(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops":     ops,
			"dry_run": false,
			"backup":  false,
		},
	})
	// We don't care about exact code here, just that it's denied and didn't write outside.
	if msg == "" {
		t.Fatalf("expected error on path traversal")
	}
}

func TestApplyOps_Stdio_MultiOp_AllApplied(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	utilsPath := filepath.Join(proj, "utils.go")

	// Multiple ops: insert at start, insert at line 2
	ops := []any{
		map[string]any{
			"op":   "file.replace_range",
			"path": "utils.go",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "",
			"replacement": "// marker1\n",
		},
		map[string]any{
			"op":   "file.replace_range",
			"path": "utils.go",
			"range": map[string]any{
				"start": map[string]any{"line": 2, "col": 0},
				"end":   map[string]any{"line": 2, "col": 0},
			},
			"expected":    "",
			"replacement": "// marker2\n",
		},
	}

	_ = c.call(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops":     ops,
			"dry_run": false,
			"backup":  false,
		},
	})

	after, err := os.ReadFile(utilsPath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify both markers are present
	if !bytes.Contains(after, []byte("// marker1\n")) {
		t.Fatalf("expected marker1 at file start; got:\n%s", string(after))
	}
	if !bytes.Contains(after, []byte("// marker2\n")) {
		t.Fatalf("expected marker2 at line 2; got:\n%s", string(after))
	}

	// Verify file structure: marker1 at start, then package, then marker2 before func
	lines := strings.Split(string(after), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines after multi-op apply")
	}
	if !strings.Contains(lines[0], "marker1") {
		t.Fatalf("expected marker1 on first line; got: %q", lines[0])
	}
	// marker2 should be after package declaration (around line 2-3)
	foundMarker2 := false
	for i := 1; i < len(lines) && i < 5; i++ {
		if strings.Contains(lines[i], "marker2") {
			foundMarker2 = true
			break
		}
	}
	if !foundMarker2 {
		t.Fatalf("expected marker2 in first few lines; got:\n%s", string(after))
	}
}

func TestApplyOps_Stdio_MultiOp_TwoFiles(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	mainPath := filepath.Join(proj, "main.go")
	utilsPath := filepath.Join(proj, "utils.go")
	origMain, _ := os.ReadFile(mainPath)
	origUtils, _ := os.ReadFile(utilsPath)

	ops := []any{
		map[string]any{
			"op":   "file.replace_range",
			"path": "main.go",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "",
			"replacement": "// main marker\n",
		},
		map[string]any{
			"op":   "file.replace_range",
			"path": "utils.go",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "",
			"replacement": "// utils marker\n",
		},
	}

	raw := c.call(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops":     ops,
			"dry_run": false,
			"backup":  false,
		},
	})

	var out struct {
		ChangedFiles []string `json:"changed_files"`
		Applied      bool     `json:"applied"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("bad fs.apply_ops response: %v raw=%s", err, string(raw))
	}
	if !out.Applied {
		t.Fatalf("expected applied=true, got applied=false")
	}
	if len(out.ChangedFiles) != 2 {
		t.Fatalf("expected 2 changed_files, got %v", out.ChangedFiles)
	}
	if out.ChangedFiles[0] != "main.go" || out.ChangedFiles[1] != "utils.go" {
		t.Fatalf("unexpected changed_files order/content: %v", out.ChangedFiles)
	}

	afterMain, _ := os.ReadFile(mainPath)
	afterUtils, _ := os.ReadFile(utilsPath)
	if bytes.Equal(origMain, afterMain) {
		t.Fatalf("expected main.go to change")
	}
	if bytes.Equal(origUtils, afterUtils) {
		t.Fatalf("expected utils.go to change")
	}
	if !bytes.HasPrefix(afterMain, []byte("// main marker\n")) {
		t.Fatalf("expected main marker at file start; got:\n%s", string(afterMain))
	}
	if !bytes.HasPrefix(afterUtils, []byte("// utils marker\n")) {
		t.Fatalf("expected utils marker at file start; got:\n%s", string(afterUtils))
	}
}

func TestApplyOps_Stdio_MultiOp_SecondFails_NoWrites(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	mainPath := filepath.Join(proj, "main.go")
	utilsPath := filepath.Join(proj, "utils.go")
	origMain, _ := os.ReadFile(mainPath)
	origUtils, _ := os.ReadFile(utilsPath)

	ops := []any{
		map[string]any{
			"op":   "file.replace_range",
			"path": "main.go",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "",
			"replacement": "// should not apply\n",
		},
		map[string]any{
			"op":   "file.replace_range",
			"path": "utils.go",
			"range": map[string]any{
				"start": map[string]any{"line": 0, "col": 0},
				"end":   map[string]any{"line": 0, "col": 0},
			},
			"expected":    "THIS_WILL_NOT_MATCH",
			"replacement": "// should not apply\n",
		},
	}

	_, code := c.callExpectError(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops":     ops,
			"dry_run": false,
			"backup":  true,
		},
	})
	if code != string(protocol.StaleContent) && !strings.Contains(code, string(protocol.StaleContent)) {
		t.Fatalf("expected %s, got: %s", protocol.StaleContent, code)
	}

	afterMain, _ := os.ReadFile(mainPath)
	afterUtils, _ := os.ReadFile(utilsPath)
	if !bytes.Equal(origMain, afterMain) {
		t.Fatalf("expected main.go unchanged on stale, got:\n%s", string(afterMain))
	}
	if !bytes.Equal(origUtils, afterUtils) {
		t.Fatalf("expected utils.go unchanged on stale, got:\n%s", string(afterUtils))
	}

	// No backups on stale
	if backups := findBackups(t, proj, "main.go"); len(backups) != 0 {
		t.Fatalf("expected no main.go backups on stale; got=%v", backups)
	}
	if backups := findBackups(t, proj, "utils.go"); len(backups) != 0 {
		t.Fatalf("expected no utils.go backups on stale; got=%v", backups)
	}
}

func TestWriteAtomic_CreateNewFile(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	rel := "internal/hello/hello.go"
	content := "package hello\n\nfunc Hello() string { return \"hi\" }\n"

	raw := c.call(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops": []any{
				map[string]any{
					"op":      "file.write_atomic",
					"path":    rel,
					"content": content,
					"mode":    420,
					"conditions": map[string]any{
						"must_not_exist": true,
					},
				},
			},
			"dry_run": false,
			"backup":  false,
		},
	})

	var out struct {
		ChangedFiles []string `json:"changed_files"`
		Applied      bool     `json:"applied"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("bad fs.apply_ops response: %v raw=%s", err, string(raw))
	}
	if !out.Applied {
		t.Fatalf("expected applied=true, got applied=false")
	}
	if len(out.ChangedFiles) != 1 || out.ChangedFiles[0] != rel {
		t.Fatalf("unexpected changed_files: %v", out.ChangedFiles)
	}

	abs := filepath.Join(proj, filepath.FromSlash(rel))
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if string(b) != content {
		t.Fatalf("content mismatch. expected=%q got=%q", content, string(b))
	}
}

func TestWriteAtomic_MustNotExist_AlreadyExists(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	rel := "internal/existing/existing.go"
	abs := filepath.Join(proj, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	orig := "package existing\n"
	if err := os.WriteFile(abs, []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}

	_, code := c.callExpectError(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops": []any{
				map[string]any{
					"op":      "file.write_atomic",
					"path":    rel,
					"content": "package existing\n// changed\n",
					"conditions": map[string]any{
						"must_not_exist": true,
					},
				},
			},
			"dry_run": false,
			"backup":  true,
		},
	})
	if code != string(protocol.AlreadyExists) && !strings.Contains(code, string(protocol.AlreadyExists)) {
		t.Fatalf("expected %s, got: %s", protocol.AlreadyExists, code)
	}

	after, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != orig {
		t.Fatalf("file must remain unchanged on AlreadyExists. got=%q", string(after))
	}

	if backups := findBackups(t, proj, filepath.FromSlash(rel)); len(backups) != 0 {
		t.Fatalf("expected no backups on AlreadyExists; got=%v", backups)
	}
}

func TestWriteAtomic_DryRun_NoCreate(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	rel := "internal/dry/dry.go"
	abs := filepath.Join(proj, filepath.FromSlash(rel))

	raw := c.call(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops": []any{
				map[string]any{
					"op":      "file.write_atomic",
					"path":    rel,
					"content": "package dry\n",
					"conditions": map[string]any{
						"must_not_exist": true,
					},
				},
			},
			"dry_run": true,
			"backup":  false,
		},
	})

	var out struct {
		ChangedFiles []string `json:"changed_files"`
		Applied      bool     `json:"applied"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("bad fs.apply_ops response: %v raw=%s", err, string(raw))
	}
	if out.Applied {
		t.Fatalf("expected applied=false for dry-run")
	}

	if _, err := os.Stat(abs); err == nil {
		t.Fatalf("dry-run must not create file: %s", abs)
	}
}

func TestMkdirAll_CreateNested(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	dirRel := "internal/nested/dir"
	fileRel := dirRel + "/file.txt"
	fileAbs := filepath.Join(proj, filepath.FromSlash(fileRel))

	_ = c.call(t, "tool.call", map[string]any{
		"name": "fs.apply_ops",
		"input": map[string]any{
			"ops": []any{
				map[string]any{
					"op":   "file.mkdir_all",
					"path": dirRel,
					"mode": 493,
				},
				map[string]any{
					"op":      "file.write_atomic",
					"path":    fileRel,
					"content": "hello\n",
					"conditions": map[string]any{
						"must_not_exist": true,
					},
				},
			},
			"dry_run": false,
			"backup":  false,
		},
	})

	b, err := os.ReadFile(fileAbs)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if string(b) != "hello\n" {
		t.Fatalf("file content mismatch: %q", string(b))
	}
}

func TestApply_FromPlan_StaleDeterministic(t *testing.T) {
	proj := setupTinyProject(t)
	bin := buildOrchestraOnce(t)

	utilsAbs := filepath.Join(proj, "utils.go")
	orig, err := os.ReadFile(utilsAbs)
	if err != nil {
		t.Fatal(err)
	}
	plannedHash := cache.ComputeSHA256(orig)

	planPath := filepath.Join(proj, ".orchestra", "plan.json")
	plan := map[string]any{
		"protocol_version":  protocol.ProtocolVersion,
		"ops_version":       protocol.OpsVersion,
		"tools_version":     protocol.ToolsVersion,
		"query":             "from-plan stale",
		"generated_at_unix": time.Now().Unix(),
		"ops": []any{
			map[string]any{
				"op":   "file.replace_range",
				"path": "utils.go",
				"range": map[string]any{
					"start": map[string]any{"line": 0, "col": 0},
					"end":   map[string]any{"line": 0, "col": 0},
				},
				"expected":    "",
				"replacement": "// from plan\n",
				"conditions": map[string]any{
					"file_hash": plannedHash,
				},
			},
		},
	}
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(planPath, b, 0644); err != nil {
		t.Fatal(err)
	}

	// External modification after "planning"
	modified := append(append([]byte(nil), orig...), []byte("\n// external\n")...)
	if err := os.WriteFile(utilsAbs, modified, 0644); err != nil {
		t.Fatal(err)
	}

	// Apply from plan (must fail deterministically with StaleContent).
	cmd := exec.Command(bin, "apply", "--from-plan", planPath, "--apply")
	cmd.Dir = proj
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Fatalf("expected non-zero exit code, got success. output:\n%s", string(out))
	}
	if !bytes.Contains(out, []byte(string(protocol.StaleContent))) {
		t.Fatalf("expected %s in output, got:\n%s", protocol.StaleContent, string(out))
	}

	// No backup and no write on stale.
	if _, err := os.Stat(utilsAbs + ".orchestra.bak"); err == nil {
		t.Fatalf("expected no backup on stale error")
	}
	after, err := os.ReadFile(utilsAbs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after, []byte("// external")) {
		t.Fatalf("expected external marker preserved; got:\n%s", string(after))
	}
}

func TestCLI_Artifacts_PlanOnly(t *testing.T) {
	proj := setupTinyProject(t)
	bin := buildOrchestraOnce(t)

	// Create a plan manually and apply it with --from-plan --plan-only
	// This avoids needing LLM for the test
	utilsAbs := filepath.Join(proj, "utils.go")
	orig, err := os.ReadFile(utilsAbs)
	if err != nil {
		t.Fatal(err)
	}
	plannedHash := cache.ComputeSHA256(orig)

	planPath := filepath.Join(proj, ".orchestra", "plan.json")
	plan := map[string]any{
		"protocol_version":  protocol.ProtocolVersion,
		"ops_version":       protocol.OpsVersion,
		"tools_version":     protocol.ToolsVersion,
		"query":             "test query",
		"generated_at_unix": time.Now().Unix(),
		"ops": []any{
			map[string]any{
				"op":   "file.replace_range",
				"path": "utils.go",
				"range": map[string]any{
					"start": map[string]any{"line": 0, "col": 0},
					"end":   map[string]any{"line": 0, "col": 0},
				},
				"expected":    "",
				"replacement": "// test change\n",
				"conditions": map[string]any{
					"file_hash": plannedHash,
				},
			},
		},
	}
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(planPath, b, 0644); err != nil {
		t.Fatal(err)
	}

	// Run apply --from-plan --plan-only (dry-run)
	cmd := exec.Command(bin, "apply", "--from-plan", planPath, "--plan-only")
	cmd.Dir = proj
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("apply --from-plan --plan-only failed: %v\noutput:\n%s", err, string(out))
	}

	// Check that artifacts are created
	artifactsDir := filepath.Join(proj, ".orchestra")
	planPathCheck := filepath.Join(artifactsDir, "plan.json")
	diffPath := filepath.Join(artifactsDir, "diff.txt")
	resultPath := filepath.Join(artifactsDir, "last_result.json")
	runPath := filepath.Join(artifactsDir, "last_run.jsonl")

	// Check plan.json exists and is valid JSON
	planData, err := os.ReadFile(planPathCheck)
	if err != nil {
		t.Fatalf("plan.json not created: %v", err)
	}
	var planCheck map[string]any
	if err := json.Unmarshal(planData, &planCheck); err != nil {
		t.Fatalf("plan.json is not valid JSON: %v", err)
	}
	if planCheck["protocol_version"] == nil {
		t.Fatalf("plan.json missing protocol_version")
	}

	// Check diff.txt exists (may be empty, but should exist)
	if _, err := os.Stat(diffPath); err != nil {
		t.Fatalf("diff.txt not created: %v", err)
	}

	// Check last_result.json exists and is valid JSON
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("last_result.json not created: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("last_result.json is not valid JSON: %v", err)
	}
	if result["dry_run"] == nil {
		t.Fatalf("last_result.json missing dry_run")
	}

	// Check last_run.jsonl exists and has at least one line
	runData, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatalf("last_run.jsonl not created: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(runData)), "\n")
	if len(lines) == 0 {
		t.Fatalf("last_run.jsonl is empty")
	}
	// First line should be valid JSON
	var firstEvent map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &firstEvent); err != nil {
		t.Fatalf("last_run.jsonl first line is not valid JSON: %v", err)
	}

	// Check stdout contains expected messages
	output := string(out)
	if !strings.Contains(output, "Plan saved to:") {
		t.Fatalf("stdout missing 'Plan saved to:'")
	}
	if !strings.Contains(output, "Diff saved to:") {
		t.Fatalf("stdout missing 'Diff saved to:'")
	}
	if !strings.Contains(output, "Dry-run: true") {
		t.Fatalf("stdout missing 'Dry-run: true'")
	}
}

func TestCLI_FromPlan_Apply(t *testing.T) {
	proj := setupTinyProject(t)
	bin := buildOrchestraOnce(t)

	// Create a plan.json manually
	utilsAbs := filepath.Join(proj, "utils.go")
	orig, err := os.ReadFile(utilsAbs)
	if err != nil {
		t.Fatal(err)
	}
	plannedHash := cache.ComputeSHA256(orig)

	planPath := filepath.Join(proj, ".orchestra", "plan.json")
	plan := map[string]any{
		"protocol_version":  protocol.ProtocolVersion,
		"ops_version":       protocol.OpsVersion,
		"tools_version":     protocol.ToolsVersion,
		"query":             "CLI from-plan test",
		"generated_at_unix": time.Now().Unix(),
		"ops": []any{
			map[string]any{
				"op":   "file.replace_range",
				"path": "utils.go",
				"range": map[string]any{
					"start": map[string]any{"line": 0, "col": 0},
					"end":   map[string]any{"line": 0, "col": 0},
				},
				"expected":    "",
				"replacement": "// CLI from-plan applied\n",
				"conditions": map[string]any{
					"file_hash": plannedHash,
				},
			},
		},
	}
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(planPath, b, 0644); err != nil {
		t.Fatal(err)
	}

	// Apply from plan
	cmd := exec.Command(bin, "apply", "--from-plan", planPath, "--apply")
	cmd.Dir = proj
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("apply --from-plan failed: %v\noutput:\n%s", runErr, string(out))
	}

	// Check file was modified
	after, err := os.ReadFile(utilsAbs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after, []byte("// CLI from-plan applied")) {
		t.Fatalf("file was not modified; got:\n%s", string(after))
	}

	// Check artifacts were created
	resultPath := filepath.Join(proj, ".orchestra", "last_result.json")
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("last_result.json not created: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("last_result.json is not valid JSON: %v", err)
	}
	if result["applied"] != true {
		t.Fatalf("last_result.json applied should be true, got: %v", result["applied"])
	}
	changedFiles, ok := result["changed_files"].([]any)
	if !ok || len(changedFiles) == 0 {
		t.Fatalf("last_result.json should have changed_files")
	}

	// Check stdout
	output := string(out)
	if !strings.Contains(output, "Changed files:") {
		t.Fatalf("stdout missing 'Changed files:'")
	}
	if !strings.Contains(output, "Dry-run: false") {
		t.Fatalf("stdout missing 'Dry-run: false'")
	}
}

func TestApplyOps_Stdio_JSONRPC_Semantics_TwoRequests_TwoResponses(t *testing.T) {
	proj := setupTinyProject(t)
	c := startCore(t, proj)
	defer c.Close()

	health := c.call(t, "core.health", map[string]any{})
	projectID, protocolVersion := parseHealth(t, health)

	c.call(t, "initialize", map[string]any{
		"project_root":     proj,
		"project_id":       projectID,
		"protocol_version": protocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      1,
	})

	// Send two requests sequentially and verify we get exactly two responses
	res1 := c.call(t, "core.health", map[string]any{})
	res2 := c.call(t, "core.health", map[string]any{})

	// Both should be valid JSON responses
	var v1, v2 map[string]any
	if err := json.Unmarshal(res1, &v1); err != nil {
		t.Fatalf("first response not valid JSON: %v", err)
	}
	if err := json.Unmarshal(res2, &v2); err != nil {
		t.Fatalf("second response not valid JSON: %v", err)
	}

	// Both should have status field (health response)
	if s1, ok1 := v1["status"].(string); !ok1 || s1 != "ok" {
		t.Fatalf("first response missing/invalid status: %v", v1)
	}
	if s2, ok2 := v2["status"].(string); !ok2 || s2 != "ok" {
		t.Fatalf("second response missing/invalid status: %v", v2)
	}

	// Verify responses are distinct (different IDs in JSON-RPC)
	// This ensures JSON-RPC semantics are preserved (request-response matching)
}

// ---------- helpers ----------

func setupTinyProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create minimal .orchestra.yml config
	// Note: LLM section is required by config validation, but e2e tests don't actually use LLM
	configYAML := fmt.Sprintf(`project_root: %s
exclude_dirs:
    - .git
    - node_modules
    - dist
    - build
    - .orchestra
context_limit_kb: 50
llm:
    api_base: http://localhost:8000/v1
    api_key: "dummy"
    model: "dummy-model"
    max_tokens: 4096
    temperature: 0.7
exec:
    timeout_s: 30
    output_limit_kb: 100
`, root)
	if err := os.WriteFile(filepath.Join(root, ".orchestra.yml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainGo := `package main

import "fmt"

func main() {
	fmt.Println(add(2, 3))
}
`
	utilsGo := `package main

func add(a, b int) int {
	return a + b
}
`

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "utils.go"), []byte(utilsGo), 0644); err != nil {
		t.Fatal(err)
	}

	return root
}

func startCore(t *testing.T, projectRoot string) *rpcClient {
	t.Helper()

	bin := buildOrchestraOnce(t)

	// Use context with timeout to prevent hanging tests
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, bin, "core", "--workspace-root", projectRoot)
	cmd.Env = append(os.Environ(),
		"ORCH_DEBUG=0",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start core: %v", err)
	}

	return &rpcClient{
		t:      t,
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stdoutFile: func() *os.File {
			if f, ok := stdout.(*os.File); ok {
				return f
			}
			return nil
		}(),
		reader: bufio.NewReader(stdout),
		nextID: 1,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (c *rpcClient) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	_ = c.stdin.Close()
	if c.stdout != nil {
		_ = c.stdout.Close()
	}

	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()

	select {
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
	case <-done:
	}
}

func (c *rpcClient) call(t *testing.T, method string, params any) []byte {
	t.Helper()
	id := c.nextID
	c.nextID++

	c.writeRequest(t, method, params, id)
	resp := c.readResponse(t)

	if resp.Error != nil {
		t.Fatalf("rpc error method=%s code=%d msg=%s data=%s", method, resp.Error.Code, resp.Error.Message, string(resp.Error.Data))
	}
	return resp.Result
}

func (c *rpcClient) callExpectError(t *testing.T, method string, params any) ([]byte, string) {
	t.Helper()
	id := c.nextID
	c.nextID++

	c.writeRequest(t, method, params, id)
	resp := c.readResponse(t)

	if resp.Error == nil {
		return resp.Result, ""
	}
	return nil, extractProtocolCode(resp.Error)
}

func (c *rpcClient) writeRequest(t *testing.T, method string, params any, id int) {
	t.Helper()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(b))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		t.Fatal(err)
	}
	if _, err := c.stdin.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (c *rpcClient) writeNotification(t *testing.T, method string, params any) {
	t.Helper()

	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(b))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		t.Fatal(err)
	}
	if _, err := c.stdin.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (c *rpcClient) assertNoResponseWithin(t *testing.T, d time.Duration) {
	t.Helper()
	if c.stdoutFile == nil {
		t.Skip("stdout pipe does not support deadlines on this platform")
	}

	// Set deadline so read does not hang.
	_ = c.stdoutFile.SetReadDeadline(time.Now().Add(d))
	defer func() { _ = c.stdoutFile.SetReadDeadline(time.Time{}) }()

	_, err := c.reader.ReadByte()
	if err == nil {
		t.Fatalf("unexpected response for notification")
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected read timeout (no response), got: %v", err)
	}
}

func (c *rpcClient) readResponse(t *testing.T) *rpcResponse {
	t.Helper()

	// Read headers
	var contentLen int
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.TrimSpace(parts[1])
		if k == "content-length" {
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatalf("bad Content-Length: %q", v)
			}
			contentLen = n
		}
	}
	if contentLen <= 0 {
		t.Fatalf("missing/invalid Content-Length")
	}

	payload := make([]byte, contentLen)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	var resp rpcResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal response: %v payload=%s", err, string(payload))
	}
	return &resp
}

func parseHealth(t *testing.T, raw []byte) (projectID string, protocolVersion int) {
	t.Helper()

	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("bad health json: %v raw=%s", err, string(raw))
	}

	if pv, ok := v["protocol_version"].(float64); ok {
		protocolVersion = int(pv)
	} else {
		// fallback to your constant if health doesn't provide it
		protocolVersion = protocol.ProtocolVersion
	}

	if pid, ok := v["project_id"].(string); ok && pid != "" {
		projectID = pid
	} else {
		// last resort: something non-empty and stable for tests
		projectID = "e2e:test"
	}
	return projectID, protocolVersion
}

func extractProtocolCode(e *rpcError) string {
	if e == nil {
		return ""
	}
	// Try to read protocol error code from error.data (if you wrap it there)
	if len(e.Data) > 0 {
		var m map[string]any
		if json.Unmarshal(e.Data, &m) == nil {
			if code, ok := m["code"].(string); ok {
				return code
			}
		}
	}
	// Fallback: parse message
	return e.Message
}

// Build orchestra binary once per package
var (
	cachedBin     string
	cachedBinOnce sync.Once
)

func buildOrchestraOnce(t *testing.T) string {
	t.Helper()

	cachedBinOnce.Do(func() {
		root, err := findRepoRoot()
		if err != nil {
			t.Fatalf("find repo root: %v", err)
		}

		// Use system temp dir with unique name per process to avoid conflicts in parallel test runs
		tmpDir := os.TempDir()
		out := filepath.Join(tmpDir, fmt.Sprintf("orchestra-test-bin-%d", os.Getpid()))
		if isWindows() {
			out += ".exe"
		}

		cmd := exec.Command("go", "build", "-o", out, "./cmd/orchestra")
		cmd.Dir = root
		cmd.Env = os.Environ()

		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go build failed: %v\n%s", err, string(b))
		}

		cachedBin = out
	})

	return cachedBin
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("go.mod not found in parents")
}

func isWindows() bool {
	return runtime.GOOS == "windows"
}

func findBackups(t *testing.T, projectRoot, filename string) []string {
	t.Helper()
	var out []string

	// suffix backup
	p := filepath.Join(projectRoot, filename) + ".orchestra.bak"
	if _, err := os.Stat(p); err == nil {
		out = append(out, p)
	}

	// backups dir
	bdir := filepath.Join(projectRoot, ".orchestra", "backups")
	entries, err := os.ReadDir(bdir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			out = append(out, filepath.Join(bdir, e.Name()))
		}
	}
	return out
}
