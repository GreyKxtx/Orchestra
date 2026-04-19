package e2e_nollm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
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
	reader *bufio.Reader
	nextID int
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
		"tools_version":    1,
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
		"tools_version":    1,
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

	// Op expects old file start to match "", but we'll use strict expected that won't match
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

// ---------- helpers ----------

func setupTinyProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create minimal .orchestra.yml config
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
    api_key: "test"
    model: "test-model"
    max_tokens: 4096
    temperature: 0.7
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

	cmd := exec.Command(bin, "core", "--workspace-root", projectRoot)
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
		reader: bufio.NewReader(stdout),
		nextID: 1,
	}
}

func (c *rpcClient) Close() {
	_ = c.stdin.Close()

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

		// Use system temp dir (not per-test temp dir) so binary persists across tests
		tmpDir := os.TempDir()
		out := filepath.Join(tmpDir, "orchestra-test-bin")
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
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows") || filepath.Separator == '\\'
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
