package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orchestra/orchestra/internal/subproc"
)

type callResult struct {
	raw json.RawMessage
	err error
}

// Client communicates with one LSP server subprocess via stdio JSON-RPC.
type Client struct {
	name string
	cmd  *exec.Cmd // nil for test connections

	wMu sync.Mutex
	w   io.WriteCloser
	r   *bufio.Reader

	idSeq   atomic.Int64
	pending sync.Map // int64 → chan callResult

	notifyCh  chan rpcMessage
	closeOnce sync.Once
	// readDone is closed by readLoop on exit so Close can join cleanly
	// instead of returning while in-flight callers still block on their
	// own ctx. M21 in audit ledger.
	readDone chan struct{}

	dead atomic.Bool

	posEncoding string // "utf-8" or "utf-16" (negotiated during initialize)

	docMu       sync.Mutex
	docVersions map[string]int // uri → current version

	// stderr is a bounded ring (default 64 KiB) carrying the server's
	// most recent stderr output. M18 + S1 in audit ledger: shared with
	// MCP via internal/subproc.StderrRing — used to be a per-package
	// re-implementation here and in mcp/client.go.
	stderr *subproc.StderrRing

	// notifyDropped counts publishDiagnostics / etc. notifications that were
	// dropped because notifyCh was full (M19). Read via DroppedNotifications.
	notifyDropped atomic.Uint64

	// Set by Manager after construction.
	DiagCache *DiagnosticsCache
}

// StderrTail returns the last <=64 KiB of the server's stderr output.
// Use for diagnostics surfacing — e.g. logging when the server crashes.
func (c *Client) StderrTail() string {
	return c.stderr.Tail()
}

// DroppedNotifications reports how many publishDiagnostics / etc. messages
// were silently discarded because notifyCh was full. Non-zero indicates a
// slow consumer or a notification storm during workspace init (M19).
func (c *Client) DroppedNotifications() uint64 {
	return c.notifyDropped.Load()
}

func newClient(name string, cmd *exec.Cmd, r io.Reader, w io.WriteCloser) *Client {
	c := &Client{
		name:        name,
		cmd:         cmd,
		w:           w,
		r:           bufio.NewReaderSize(r, 64*1024),
		notifyCh:    make(chan rpcMessage, 256),
		posEncoding: "utf-16", // conservative default until initialize
		docVersions: make(map[string]int),
		readDone:    make(chan struct{}),
		stderr:      subproc.NewStderrRing(0),
	}
	go c.readLoop()
	return c
}

// Start launches command as a subprocess and performs the LSP initialize handshake.
func Start(ctx context.Context, name string, command []string, env map[string]string, rootURI string, initOptions any) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("lsp: empty command for server %q", name)
	}
	cmd := exec.Command(command[0], command[1:]...) //nolint:gosec
	// H3 in audit ledger: inherit the parent's environment FIRST, then layer
	// user-supplied overrides on top. Without this, the moment a user sets
	// even one `env:` entry in .orchestra.yml, PATH/HOME/GOPATH disappear
	// and most LSP servers fail to start (no PATH for spawned tools, no
	// HOME for cache).
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), nil...) // copy
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	// H13 parity (audit ledger) + S2 consolidation: start the LSP server in
	// its own process group so subproc.KillProcessTree (in Close) reaches
	// every descendant. tsserver, pyright, etc. spawn helpers that would
	// otherwise survive shutdown.
	subproc.SetProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdin pipe for %q: %w", name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdout pipe for %q: %w", name, err)
	}
	// M18 in audit ledger: pipe stderr to a 64 KiB ring buffer instead of
	// discarding. Gopls / tsserver crash dumps, "missing go.mod" notices,
	// license messages were previously invisible — operator had no way to
	// see why the server failed. The ring buffer keeps the last ~64 KiB
	// available via Client.StderrTail() for surfacing in core.health or
	// debug logs without unbounded growth.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stderr pipe for %q: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start %q: %w", name, err)
	}

	c := newClient(name, cmd, stdout, stdin)
	// Drain stderr into the ring buffer in its own goroutine so the
	// subprocess's stderr pipe never fills and blocks the server.
	go c.stderr.Drain(stderrPipe)
	if err := c.initialize(ctx, rootURI, initOptions); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("lsp: initialize %q: %w", name, err)
	}
	return c, nil
}

// StartFromConn creates a client from an existing connection (used in tests).
func StartFromConn(name string, conn io.ReadWriteCloser, rootURI string, initOptions any) (*Client, error) {
	c := newClient(name, nil, conn, conn)
	if err := c.initialize(context.Background(), rootURI, initOptions); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("lsp: initialize %q: %w", name, err)
	}
	return c, nil
}

func (c *Client) initialize(ctx context.Context, rootURI string, initOptions any) error {
	params := map[string]any{
		// L7 in audit ledger: send the real parent PID so servers can
		// monitor it and exit when Orchestra crashes without Close. The
		// spec says integer-or-null; `0` was an invalid placeholder that
		// stricter servers (pyright's older versions) rejected outright.
		"processId":  os.Getpid(),
		"clientInfo": map[string]string{"name": "orchestra", "version": "vnext"},
		"rootUri":    rootURI,
		"workspaceFolders": []map[string]string{
			{"uri": rootURI, "name": "workspace"},
		},
		"capabilities": map[string]any{
			"general": map[string]any{
				"positionEncodings": []string{"utf-8", "utf-16"},
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"dynamicRegistration": false,
					"didSave":             false,
				},
				"publishDiagnostics": map[string]any{"relatedInformation": false},
				"definition":         map[string]any{"linkSupport": true},
				"references":         map[string]any{},
				"hover":              map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
				"rename":             map[string]any{"prepareSupport": false},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
			},
			"workspace": map[string]any{"workspaceFolders": true},
		},
	}
	if initOptions != nil {
		params["initializationOptions"] = initOptions
	}

	raw, err := c.request(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	var result struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &result); err == nil {
		switch result.Capabilities.PositionEncoding {
		case "utf-8", "UTF-8":
			c.posEncoding = "utf-8"
		default:
			c.posEncoding = "utf-16"
		}
	}

	return c.notify(ctx, "initialized", map[string]any{})
}

// Request sends a JSON-RPC request and waits for the response.
func (c *Client) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.request(ctx, method, params)
}

func (c *Client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.dead.Load() {
		return nil, fmt.Errorf("lsp: server %q is dead", c.name)
	}
	id := c.idSeq.Add(1)
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("lsp: marshal params: %w", err)
	}
	msg := rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: paramsRaw}

	ch := make(chan callResult, 1)
	c.pending.Store(id, ch)

	if err := c.writeMsg(msg); err != nil {
		c.pending.Delete(id)
		return nil, err
	}

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.raw, nil
	case <-ctx.Done():
		c.pending.Delete(id)
		return nil, ctx.Err()
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	return c.notify(ctx, method, params)
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	if c.dead.Load() {
		return fmt.Errorf("lsp: server %q is dead", c.name)
	}
	// M23 in audit ledger: respect caller's ctx even before we attempt
	// the (potentially blocking) write. Useful when the LSP server has
	// stopped reading stdin — the writer would block on wMu otherwise.
	if err := ctx.Err(); err != nil {
		return err
	}
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("lsp: marshal params: %w", err)
	}
	return c.writeMsg(rpcMessage{JSONRPC: "2.0", Method: method, Params: paramsRaw})
}

func (c *Client) writeMsg(msg rpcMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("lsp: marshal: %w", err)
	}
	c.wMu.Lock()
	defer c.wMu.Unlock()
	return WriteMessage(c.w, b)
}

// PosEncoding returns the negotiated position encoding.
func (c *Client) PosEncoding() string { return c.posEncoding }

// IsDead reports whether the server has exited.
func (c *Client) IsDead() bool { return c.dead.Load() }

// Notifications returns a channel of inbound server notifications.
func (c *Client) Notifications() <-chan rpcMessage { return c.notifyCh }

// IsOpen reports whether the document is currently open in the server.
func (c *Client) IsOpen(uri string) bool {
	c.docMu.Lock()
	defer c.docMu.Unlock()
	_, ok := c.docVersions[uri]
	return ok
}

// DocVersion returns the current document version (0 if not open).
func (c *Client) DocVersion(uri string) int {
	c.docMu.Lock()
	defer c.docMu.Unlock()
	return c.docVersions[uri]
}

// DidOpen notifies the server that a document was opened.
func (c *Client) DidOpen(ctx context.Context, uri, languageID, content string) error {
	c.docMu.Lock()
	c.docVersions[uri] = 1
	c.docMu.Unlock()
	return c.notify(ctx, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": languageID, "version": 1, "text": content,
		},
	})
}

// DidChange notifies the server of a full-document change (increments version).
func (c *Client) DidChange(ctx context.Context, uri, content string) error {
	c.docMu.Lock()
	v := c.docVersions[uri] + 1
	c.docVersions[uri] = v
	c.docMu.Unlock()
	return c.notify(ctx, "textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": v},
		"contentChanges": []map[string]any{{"text": content}},
	})
}

// DidClose notifies the server that a document was closed.
func (c *Client) DidClose(ctx context.Context, uri string) error {
	c.docMu.Lock()
	delete(c.docVersions, uri)
	c.docMu.Unlock()
	return c.notify(ctx, "textDocument/didClose", map[string]any{
		"textDocument": map[string]string{"uri": uri},
	})
}

// Close gracefully shuts down the language server.
func (c *Client) Close() error {
	if c.dead.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = c.request(ctx, "shutdown", nil)
	_ = c.notify(ctx, "exit", nil)

	c.wMu.Lock()
	_ = c.w.Close()
	c.wMu.Unlock()

	if c.cmd != nil {
		done := make(chan struct{})
		go func() { _ = c.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			// H13 parity (audit ledger) + S2 consolidation: kill the process
			// tree so helpers the LSP server spawned (tsc, builders) don't
			// survive.
			subproc.KillProcessTree(c.cmd)
		}
	}
	// M21 in audit ledger: wait for readLoop to fully exit so any in-flight
	// request that's racing on its own ctx sees the drained "server died"
	// error before Close returns to the caller. Bounded by 1s — in normal
	// operation readLoop returns the moment c.r returns EOF after w.Close.
	select {
	case <-c.readDone:
	case <-time.After(time.Second):
	}
	return nil
}

func (c *Client) readLoop() {
	// L10 in audit ledger: panic recovery so a malformed server payload
	// that explodes inside json.Unmarshal (or any future parser bug)
	// doesn't crash the whole process. Mark the client dead and let the
	// deferred cleanup drain pending requests with the standard "died"
	// error.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "lsp: server %q readLoop panicked: %v\n", c.name, r)
		}
	}()
	defer func() {
		c.dead.Store(true)
		close(c.readDone)
		c.closeOnce.Do(func() { close(c.notifyCh) })
		// Wake all pending requests with an error.
		c.pending.Range(func(key, value any) bool {
			ch := value.(chan callResult)
			select {
			case ch <- callResult{err: fmt.Errorf("lsp: server %q died", c.name)}:
			default:
			}
			return true
		})
	}()

	for {
		body, err := ReadMessage(c.r)
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}

		if msg.ID != nil && msg.Method == "" {
			// Response to our request.
			id := *msg.ID
			if v, ok := c.pending.LoadAndDelete(id); ok {
				ch := v.(chan callResult)
				if msg.Error != nil {
					ch <- callResult{err: msg.Error}
				} else {
					ch <- callResult{raw: msg.Result}
				}
			}
			continue
		}

		if msg.Method != "" {
			if msg.ID != nil {
				// Server → client request: reply with a method-specific shape.
				// L8 in audit ledger: workspace/configuration expects an
				// array (one entry per ConfigurationItem the server asked
				// about); replying `null` made strict servers crash.
				// Default for unknown methods stays `null` (we don't
				// implement workspace/applyEdit, window/showMessageRequest,
				// etc. yet — those servers fall back gracefully).
				var result json.RawMessage = json.RawMessage(`null`)
				if msg.Method == "workspace/configuration" {
					// Return one null per requested item — gopls / tsserver
					// both accept this as "no client-side config".
					var p struct {
						Items []json.RawMessage `json:"items"`
					}
					n := 1
					if json.Unmarshal(msg.Params, &p) == nil && len(p.Items) > 0 {
						n = len(p.Items)
					}
					arr := make([]json.RawMessage, n)
					for i := range arr {
						arr[i] = json.RawMessage(`null`)
					}
					if b, err := json.Marshal(arr); err == nil {
						result = b
					}
				}
				resp := rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: result}
				_ = c.writeMsg(resp)
			}
			// Forward all method messages as notifications for side-effects.
			select {
			case c.notifyCh <- msg:
			default:
				// M19 in audit ledger: count drops so operators can detect
				// a slow consumer (or workspace-init notification storm) via
				// DroppedNotifications(). Drop oldest and retry.
				c.notifyDropped.Add(1)
				select {
				case <-c.notifyCh:
				default:
				}
				select {
				case c.notifyCh <- msg:
				default:
				}
			}
		}
	}
}
