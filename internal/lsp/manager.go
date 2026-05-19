package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LSPServerConfig is the configuration for one language server.
// Imported inline to avoid a dependency on internal/config from this package.
type LSPServerConfig struct {
	Language    string            `yaml:"language"`
	Extensions  []string          `yaml:"extensions"`
	Command     []string          `yaml:"command"`
	Env         map[string]string `yaml:"env,omitempty"`
	Disabled    bool              `yaml:"disabled,omitempty"`
	InitOptions map[string]any    `yaml:"init_options,omitempty"`
}

// LSPConfig is the top-level LSP configuration block.
type LSPConfig struct {
	Enabled              *bool             `yaml:"enabled,omitempty"`
	Servers              []LSPServerConfig `yaml:"servers,omitempty"`
	DiagnosticsTimeoutMS int               `yaml:"diagnostics_timeout_ms,omitempty"`
}

// ToolLocation is an LSP location converted to workspace-relative, 1-based coordinates.
type ToolLocation struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
}

// ToolDiagnostic is a diagnostic with 1-based positions and a string severity.
type ToolDiagnostic struct {
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
	Severity  string `json:"severity"` // "error" | "warning" | "information" | "hint"
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
}

// ToolSymbol is a document symbol in workspace-relative, 1-based coordinates.
type ToolSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "function", "method", "type", etc.
	Detail    string `json:"detail,omitempty"`
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
}

// ProposedEdit is one proposed rename edit returned to the agent.
type ProposedEdit struct {
	Path      string `json:"path"` // workspace-relative
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
	NewText   string `json:"new_text"`
}

type serverEntry struct {
	cfg   LSPServerConfig
	diags *DiagnosticsCache
	exts  map[string]bool

	// mu guards client + restartCount on the slow / restart path.
	// The fast path (alive client) reads client without the lock and is
	// safe because client is never set to nil after construction — only
	// replaced atomically while mu is held. N1 in audit ledger (Sprint 6).
	mu           sync.Mutex
	client       *Client
	restartCount int // H6 in audit ledger: bounded lazy restart on crash
}

// Manager manages one LSP client per language, routing by file extension.
type Manager struct {
	workspaceRoot string
	servers       []*serverEntry
	diagTimeoutMS int
}

// NewManager starts all enabled servers and returns any per-server start errors (non-fatal).
func NewManager(workspaceRoot string, cfg LSPConfig) (*Manager, []error) {
	m := &Manager{
		workspaceRoot: workspaceRoot,
		diagTimeoutMS: cfg.DiagnosticsTimeoutMS,
	}
	if m.diagTimeoutMS <= 0 {
		m.diagTimeoutMS = 1500
	}
	if cfg.Enabled != nil && !*cfg.Enabled {
		return m, nil
	}

	rootURI := PathToURI(workspaceRoot)
	var errs []error

	// C4 (audit ledger): cap LSP init at 30s per server so a hung gopls /
	// tsserver / pyright cannot block Core / apply / TUI startup forever.
	// A timed-out server is reported as a non-fatal start error and skipped;
	// the rest of Orchestra runs without LSP support for that language.
	const lspStartTimeout = 30 * time.Second

	for _, sc := range cfg.Servers {
		if sc.Disabled || len(sc.Command) == 0 {
			continue
		}
		diags := NewDiagnosticsCache()
		startCtx, cancel := context.WithTimeout(context.Background(), lspStartTimeout)
		c, err := Start(startCtx, sc.Language, sc.Command, sc.Env, rootURI, sc.InitOptions)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("lsp server %q: %w", sc.Language, err))
			continue
		}
		c.DiagCache = diags
		go dispatchNotifications(c, diags)

		exts := make(map[string]bool, len(sc.Extensions))
		for _, ext := range sc.Extensions {
			exts[strings.ToLower(ext)] = true
		}
		m.servers = append(m.servers, &serverEntry{cfg: sc, client: c, diags: diags, exts: exts})
	}
	return m, errs
}

// Close shuts down all managed servers.
func (m *Manager) Close() {
	for _, s := range m.servers {
		if s.client != nil && !s.client.IsDead() {
			_ = s.client.Close()
		}
	}
	m.servers = nil
}

// IsEmpty reports whether any servers are running.
func (m *Manager) IsEmpty() bool { return m == nil || len(m.servers) == 0 }

// ForTest creates a Manager from a pre-started *Client, for use in tests.
// The client is assumed to have already completed the initialize handshake.
func ForTest(workspaceRoot string, c *Client, extensions []string, diagTimeoutMS int) *Manager {
	exts := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		exts[strings.ToLower(ext)] = true
	}
	diags := NewDiagnosticsCache()
	c.DiagCache = diags
	go dispatchNotifications(c, diags)
	if diagTimeoutMS <= 0 {
		diagTimeoutMS = 1500
	}
	m := &Manager{workspaceRoot: workspaceRoot, diagTimeoutMS: diagTimeoutMS}
	m.servers = append(m.servers, &serverEntry{
		cfg:    LSPServerConfig{Extensions: extensions},
		client: c,
		diags:  diags,
		exts:   exts,
	})
	return m
}

// dispatchNotifications reads notifications from c and routes them to diags.
func dispatchNotifications(c *Client, diags *DiagnosticsCache) {
	for msg := range c.Notifications() {
		if msg.Method == "textDocument/publishDiagnostics" {
			diags.HandleNotification(msg.Params)
		}
	}
}

// maxLSPRestarts caps how many times serverForPath will try to lazily
// restart a dead server per Core lifetime. After this, the language is
// considered unsupported for the rest of the session. H6 in audit ledger.
const maxLSPRestarts = 3

func (m *Manager) serverForPath(relPath string) (*serverEntry, error) {
	if m.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	for _, s := range m.servers {
		if !s.exts[ext] {
			continue
		}
		// Fast path: alive client, no lock.
		if c := s.client; c != nil && !c.IsDead() {
			return s, nil
		}
		// Slow path: serialize restart attempts for this entry so that
		// concurrent calls don't both increment restartCount past the cap
		// and don't both spawn a fresh subprocess. N1 in audit ledger.
		if ok := s.tryRestart(m.workspaceRoot); ok {
			return s, nil
		}
	}
	return nil, fmt.Errorf("lsp: no server configured for %q files (ext=%q)", relPath, ext)
}

// tryRestart serializes lazy-restart attempts for one serverEntry. Returns
// true if the entry has an alive client when it returns (either someone
// else restarted while we waited, or we restarted successfully).
func (s *serverEntry) tryRestart(workspaceRoot string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under the lock: another goroutine may have already
	// restarted while we were blocked on Lock.
	if s.client != nil && !s.client.IsDead() {
		return true
	}
	if s.restartCount >= maxLSPRestarts {
		return false
	}
	s.restartCount++
	rootURI := PathToURI(workspaceRoot)
	startCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	fresh, err := Start(startCtx, s.cfg.Language, s.cfg.Command, s.cfg.Env, rootURI, s.cfg.InitOptions)
	cancel()
	if err != nil {
		// Mark this entry permanently dead by exhausting the budget.
		s.restartCount = maxLSPRestarts
		return false
	}
	fresh.DiagCache = s.diags
	go dispatchNotifications(fresh, s.diags)
	s.client = fresh
	return true
}

func (m *Manager) ensureOpen(ctx context.Context, s *serverEntry, relPath string) error {
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)
	if s.client.IsOpen(uri) {
		return nil
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("lsp: read %s: %w", relPath, err)
	}
	return s.client.DidOpen(ctx, uri, langIDFromExt(filepath.Ext(relPath)), string(content))
}

// DidClose sends textDocument/didClose for every server that has the
// document at relPath open, and forgets cached diagnostics for that URI.
// H7 in audit ledger: previously fs.delete / fs.rename did nothing on the
// LSP side, leaving stale open-document state for the deleted file and
// stale diagnostics in DiagnosticsCache forever. Safe to call even when
// the document isn't open on any server (silent no-op).
func (m *Manager) DidClose(ctx context.Context, relPath string) {
	if m.IsEmpty() {
		return
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)
	for _, s := range m.servers {
		if s.client == nil || s.client.IsDead() {
			continue
		}
		if !s.client.IsOpen(uri) {
			continue
		}
		_ = s.client.DidClose(ctx, uri)
		if s.diags != nil {
			s.diags.Forget(uri)
		}
	}
}

// readLineText returns the raw text of `line` (0-based) from the file at
// relPath. Returns "" on any read or out-of-range error — callers pass the
// result to pos.ToLSP where "" simply means "fall through unchanged", which
// is safe for ASCII columns. H4 in audit ledger: previously every ToLSP
// callsite passed "" hard-coded, so non-ASCII columns sent to UTF-16
// servers were wrong by the number of multi-byte runes before them.
func (m *Manager) readLineText(relPath string, line int) string {
	if line < 0 {
		return ""
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	content, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	// Walk for the Nth line manually so we don't allocate the full split
	// slice for a single-line lookup.
	cur := 0
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		if cur == line {
			return string(content[start:i])
		}
		cur++
		start = i + 1
	}
	if cur == line { // last line (no trailing newline)
		return string(content[start:])
	}
	return ""
}

// Definition returns the definition location(s) of the symbol at pos.
func (m *Manager) Definition(ctx context.Context, relPath string, pos ToolPosition) ([]ToolLocation, error) {
	s, err := m.serverForPath(relPath)
	if err != nil {
		return nil, err
	}
	if err := m.ensureOpen(ctx, s, relPath); err != nil {
		return nil, err
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)

	raw, err := s.client.Request(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]string{"uri": uri},
		"position":     pos.ToLSP(s.client.PosEncoding(), m.readLineText(relPath, pos.Line)),
	})
	if err != nil {
		return nil, fmt.Errorf("lsp.definition: %w", err)
	}
	locs, err := parseLocations(raw)
	if err != nil {
		return nil, fmt.Errorf("lsp.definition parse: %w", err)
	}
	return m.locsToTool(locs), nil
}

// References returns all references to the symbol at pos.
func (m *Manager) References(ctx context.Context, relPath string, pos ToolPosition, includeDecl bool) ([]ToolLocation, error) {
	s, err := m.serverForPath(relPath)
	if err != nil {
		return nil, err
	}
	if err := m.ensureOpen(ctx, s, relPath); err != nil {
		return nil, err
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)

	raw, err := s.client.Request(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]string{"uri": uri},
		"position":     pos.ToLSP(s.client.PosEncoding(), m.readLineText(relPath, pos.Line)),
		"context":      map[string]bool{"includeDeclaration": includeDecl},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp.references: %w", err)
	}
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err != nil {
		if string(raw) == "null" {
			return []ToolLocation{}, nil
		}
		return nil, fmt.Errorf("lsp.references parse: %w", err)
	}
	return m.locsToTool(locs), nil
}

// Hover returns hover text for the symbol at pos.
func (m *Manager) Hover(ctx context.Context, relPath string, pos ToolPosition) (string, error) {
	s, err := m.serverForPath(relPath)
	if err != nil {
		return "", err
	}
	if err := m.ensureOpen(ctx, s, relPath); err != nil {
		return "", err
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)

	raw, err := s.client.Request(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]string{"uri": uri},
		"position":     pos.ToLSP(s.client.PosEncoding(), m.readLineText(relPath, pos.Line)),
	})
	if err != nil {
		return "", fmt.Errorf("lsp.hover: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	return extractHoverText(raw), nil
}

// GetDiagnostics returns current diagnostics for relPath.
// Waits briefly for the initial diagnostics push if none are cached.
func (m *Manager) GetDiagnostics(ctx context.Context, relPath string) ([]ToolDiagnostic, error) {
	s, err := m.serverForPath(relPath)
	if err != nil {
		return nil, err
	}
	if err := m.ensureOpen(ctx, s, relPath); err != nil {
		return nil, err
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)

	diags := s.diags.Get(uri)
	if diags == nil {
		tctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		diags = s.diags.WaitForUpdate(tctx, uri)
	}
	return diagsToTool(diags), nil
}

// Rename returns proposed edits for renaming the symbol at pos to newName.
// The edits are returned as ProposedEdit slices; the agent applies them via fs.edit/fs.write.
func (m *Manager) Rename(ctx context.Context, relPath string, pos ToolPosition, newName string) ([]ProposedEdit, error) {
	s, err := m.serverForPath(relPath)
	if err != nil {
		return nil, err
	}
	if err := m.ensureOpen(ctx, s, relPath); err != nil {
		return nil, err
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)

	raw, err := s.client.Request(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]string{"uri": uri},
		"position":     pos.ToLSP(s.client.PosEncoding(), m.readLineText(relPath, pos.Line)),
		"newName":      newName,
	})
	if err != nil {
		return nil, fmt.Errorf("lsp.rename: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("lsp.rename: server returned no edits")
	}
	var we WorkspaceEdit
	if err := json.Unmarshal(raw, &we); err != nil {
		return nil, fmt.Errorf("lsp.rename parse: %w", err)
	}
	return m.workspaceEditToProposed(we), nil
}

// DocumentSymbols returns the outline symbols for relPath via textDocument/documentSymbol.
// Returns nil (not an error) if no server handles the file or the server returns nothing.
func (m *Manager) DocumentSymbols(ctx context.Context, relPath string) ([]ToolSymbol, error) {
	s, err := m.serverForPath(relPath)
	if err != nil {
		return nil, err
	}
	if err := m.ensureOpen(ctx, s, relPath); err != nil {
		return nil, err
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)

	raw, err := s.client.Request(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]string{"uri": uri},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp.documentSymbol: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	return parseDocSymbols(raw), nil
}

// SyncAndDiagnose notifies the server of new file content and waits for diagnostics.
// Returns nil (not an error) if no server handles the file or on timeout.
//
// M17 in audit ledger: DidChange / DidOpen errors are logged at the
// Manager level (via the caller's stderr through fmt.Errorf) so the
// operator at least sees them in the agent logs — the function still
// returns nil to keep the diagnostics-empty contract intact for callers
// that treat "no diagnostics" as "all clean".
func (m *Manager) SyncAndDiagnose(ctx context.Context, relPath, content string) []ToolDiagnostic {
	s, err := m.serverForPath(relPath)
	if err != nil {
		return nil
	}
	absPath := filepath.Join(m.workspaceRoot, filepath.FromSlash(relPath))
	uri := PathToURI(absPath)

	timeout := time.Duration(m.diagTimeoutMS) * time.Millisecond
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if s.client.IsOpen(uri) {
		if err := s.client.DidChange(tctx, uri, content); err != nil {
			fmt.Fprintf(os.Stderr, "lsp: SyncAndDiagnose: DidChange %s: %v\n", relPath, err)
			return nil
		}
	} else {
		langID := langIDFromExt(filepath.Ext(relPath))
		if err := s.client.DidOpen(tctx, uri, langID, content); err != nil {
			fmt.Fprintf(os.Stderr, "lsp: SyncAndDiagnose: DidOpen %s: %v\n", relPath, err)
			return nil
		}
	}
	return diagsToTool(s.diags.WaitForUpdate(tctx, uri))
}

// --- helpers ---

func (m *Manager) locsToTool(locs []Location) []ToolLocation {
	out := make([]ToolLocation, 0, len(locs))
	for _, loc := range locs {
		absPath, err := URIToPath(loc.URI)
		if err != nil {
			continue
		}
		relPath, err := filepath.Rel(m.workspaceRoot, absPath)
		if err != nil {
			continue
		}
		// M20 in audit ledger: drop out-of-workspace results (stdlib defs,
		// dependency sources at $GOPATH/pkg/mod, …). Their paths come back
		// as `../../usr/local/go/src/fmt/print.go` which downstream tools
		// reject as path-traversal. The model would see them and try to
		// `fs.read` — guaranteed failure. Better to omit than to mislead.
		if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
			continue
		}
		out = append(out, ToolLocation{
			Path:      filepath.ToSlash(relPath),
			StartLine: int(loc.Range.Start.Line) + 1,
			StartCol:  int(loc.Range.Start.Character) + 1,
			EndLine:   int(loc.Range.End.Line) + 1,
			EndCol:    int(loc.Range.End.Character) + 1,
		})
	}
	return out
}

func diagsToTool(diags []Diagnostic) []ToolDiagnostic {
	out := make([]ToolDiagnostic, 0, len(diags))
	for _, d := range diags {
		out = append(out, ToolDiagnostic{
			StartLine: int(d.Range.Start.Line) + 1,
			StartCol:  int(d.Range.Start.Character) + 1,
			EndLine:   int(d.Range.End.Line) + 1,
			EndCol:    int(d.Range.End.Character) + 1,
			Severity:  d.Severity.String(),
			Source:    d.Source,
			Message:   d.Message,
		})
	}
	return out
}

func (m *Manager) workspaceEditToProposed(we WorkspaceEdit) []ProposedEdit {
	editsPerURI := make(map[string][]TextEdit)
	if len(we.DocumentChanges) > 0 {
		for _, dc := range we.DocumentChanges {
			editsPerURI[dc.TextDocument.URI] = append(editsPerURI[dc.TextDocument.URI], dc.Edits...)
		}
	} else {
		for uri, edits := range we.Changes {
			editsPerURI[uri] = edits
		}
	}
	var out []ProposedEdit
	for uri, edits := range editsPerURI {
		absPath, err := URIToPath(uri)
		if err != nil {
			continue
		}
		relPath, _ := filepath.Rel(m.workspaceRoot, absPath)
		for _, edit := range edits {
			out = append(out, ProposedEdit{
				Path:      filepath.ToSlash(relPath),
				StartLine: int(edit.Range.Start.Line) + 1,
				StartCol:  int(edit.Range.Start.Character) + 1,
				EndLine:   int(edit.Range.End.Line) + 1,
				EndCol:    int(edit.Range.End.Character) + 1,
				NewText:   edit.NewText,
			})
		}
	}
	return out
}

// parseLocations handles the polymorphic definition response.
func parseLocations(raw json.RawMessage) ([]Location, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var arr []Location
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var single Location
	if err := json.Unmarshal(raw, &single); err == nil && single.URI != "" {
		return []Location{single}, nil
	}
	// LocationLink[]
	var links []LocationLink
	if err := json.Unmarshal(raw, &links); err == nil {
		out := make([]Location, 0, len(links))
		for _, l := range links {
			out = append(out, Location{URI: l.TargetURI, Range: l.TargetSelectionRange})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected definition response")
}

// extractHoverText converts the polymorphic hover result to a string.
func extractHoverText(raw json.RawMessage) string {
	// Try { contents: MarkupContent }
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := json.Unmarshal(raw, &h); err == nil && len(h.Contents) > 0 {
		var mc MarkupContent
		if err := json.Unmarshal(h.Contents, &mc); err == nil && mc.Value != "" {
			return mc.Value
		}
		var s string
		if err := json.Unmarshal(h.Contents, &s); err == nil {
			return s
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(h.Contents, &arr); err == nil {
			parts := make([]string, 0, len(arr))
			for _, item := range arr {
				var s string
				if err := json.Unmarshal(item, &s); err == nil {
					parts = append(parts, s)
					continue
				}
				var ms struct {
					Value string `json:"value"`
				}
				if err := json.Unmarshal(item, &ms); err == nil {
					parts = append(parts, ms.Value)
				}
			}
			return strings.Join(parts, "\n\n")
		}
	}
	// Try MarkupContent directly
	var mc MarkupContent
	if err := json.Unmarshal(raw, &mc); err == nil && mc.Value != "" {
		return mc.Value
	}
	return ""
}

// parseDocSymbols handles both DocumentSymbol[] (hierarchical) and SymbolInformation[] (flat).
func parseDocSymbols(raw json.RawMessage) []ToolSymbol {
	// Probe: if first element has a "location" key → SymbolInformation[].
	var probe []struct {
		Location *Location `json:"location"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && len(probe) > 0 && probe[0].Location != nil {
		var symInfos []SymbolInformation
		if err := json.Unmarshal(raw, &symInfos); err == nil {
			return symInfosToTool(symInfos)
		}
	}
	// Otherwise → DocumentSymbol[].
	var docSyms []DocumentSymbol
	if err := json.Unmarshal(raw, &docSyms); err == nil {
		return flattenDocSymbols(docSyms, nil)
	}
	return nil
}

// flattenDocSymbols recursively flattens the hierarchical DocumentSymbol tree.
func flattenDocSymbols(syms []DocumentSymbol, out []ToolSymbol) []ToolSymbol {
	for _, s := range syms {
		out = append(out, ToolSymbol{
			Name:      s.Name,
			Kind:      s.Kind.String(),
			Detail:    s.Detail,
			StartLine: int(s.SelectionRange.Start.Line) + 1,
			StartCol:  int(s.SelectionRange.Start.Character) + 1,
			EndLine:   int(s.SelectionRange.End.Line) + 1,
			EndCol:    int(s.SelectionRange.End.Character) + 1,
		})
		if len(s.Children) > 0 {
			out = flattenDocSymbols(s.Children, out)
		}
	}
	return out
}

func symInfosToTool(syms []SymbolInformation) []ToolSymbol {
	out := make([]ToolSymbol, len(syms))
	for i, s := range syms {
		out[i] = ToolSymbol{
			Name:      s.Name,
			Kind:      s.Kind.String(),
			StartLine: int(s.Location.Range.Start.Line) + 1,
			StartCol:  int(s.Location.Range.Start.Character) + 1,
			EndLine:   int(s.Location.Range.End.Line) + 1,
			EndCol:    int(s.Location.Range.End.Character) + 1,
		}
	}
	return out
}

func langIDFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	default:
		return "plaintext"
	}
}
