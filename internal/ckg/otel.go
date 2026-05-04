package ckg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Resolve status constants written to spans.resolve_status.
const (
	ResolveStatusResolved          = "resolved"
	ResolveStatusNoCodeAttrs       = "no_code_attrs"
	ResolveStatusPathNotUnderRoot  = "path_not_under_root"
	ResolveStatusPathNotInCKG      = "path_not_in_ckg"
	ResolveStatusNoNodeAtLine      = "no_node_at_line"
)

// ---- OTLP JSON wire types ----

type otlpPayload struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type otlpScopeSpan struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId"`
	Name         string          `json:"name"`
	StartNano    json.Number     `json:"startTimeUnixNano"` // arrives as string per OTLP-JSON spec
	EndNano      json.Number     `json:"endTimeUnixNano"`
	Status       otlpStatus      `json:"status"`
	Attributes   []otlpAttribute `json:"attributes"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type otlpAttribute struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// otlpAttrString decodes an OTLP attribute value union to a string.
// All numeric types (intValue, doubleValue) arrive as JSON strings per spec.
func otlpAttrString(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	for _, key := range []string{"stringValue", "intValue", "boolValue", "doubleValue"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		// Try string first (covers intValue arriving as "123").
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s
		}
		// Bare JSON number or bool.
		var n json.Number
		if err := json.Unmarshal(v, &n); err == nil {
			return n.String()
		}
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			if b {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

func attrsToMap(attrs []otlpAttribute) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		if v := otlpAttrString(a.Value); v != "" {
			m[a.Key] = v
		}
	}
	return m
}

func nanoToTime(n json.Number) time.Time {
	s := n.String()
	if s == "" || s == "0" {
		return time.Time{}
	}
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}

// ---- Path normalisation ----

// normalizeOTelPath converts a raw code.filepath value from an OTel span into
// the CKG canonical form: forward-slash, relative from rootDir.
// Returns ("", false) when the path cannot be made relative to rootDir.
func normalizeOTelPath(raw, rootDir string) (string, bool) {
	if raw == "" {
		return "", false
	}

	// Normalise separators before any platform-specific operations.
	unified := strings.ReplaceAll(raw, `\`, "/")
	native := filepath.FromSlash(unified)

	var abs string
	if filepath.IsAbs(native) {
		abs = filepath.Clean(native)
	} else {
		clean := strings.TrimPrefix(unified, "./")
		abs = filepath.Join(rootDir, filepath.FromSlash(clean))
	}

	rootClean := filepath.Clean(rootDir)

	absCheck := abs
	rootCheck := rootClean
	if runtime.GOOS == "windows" {
		absCheck = strings.ToLower(abs)
		rootCheck = strings.ToLower(rootClean)
	}

	sep := string(os.PathSeparator)
	if absCheck != rootCheck && !strings.HasPrefix(absCheck, rootCheck+sep) {
		return "", false
	}

	rel, err := filepath.Rel(rootClean, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+sep) {
		return "", false
	}

	return filepath.ToSlash(rel), true
}

// ---- Public data types ----

// TraceData is the parsed representation of one OTLP trace batch.
type TraceData struct {
	TraceID    string
	Service    string
	StartedAt  time.Time
	DurationMS int64
	Spans      []SpanData
}

// SpanData holds the fields of one OTel span after parsing.
type SpanData struct {
	SpanID       string
	ParentSpanID string
	Name         string
	Service      string
	CodeFile     string // CKG-canonical slash-relative path (empty if unknown)
	CodeLineno   int
	CodeFunc     string
	StartedAt    time.Time
	DurationMS   int64
	Status       string
	ErrorMsg     string
	Attributes   string // JSON blob of all attributes
}

// ---- OTLP parser ----

// ParseOTLPJSON parses an OTLP JSON export and returns one TraceData per
// unique trace_id found in the payload. rootDir is used for path normalisation.
func ParseOTLPJSON(data []byte, rootDir string) ([]TraceData, error) {
	var payload otlpPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse OTLP JSON: %w", err)
	}

	type entry struct {
		td    *TraceData
		index int
	}
	byID := map[string]*entry{}
	var order []string

	for _, rs := range payload.ResourceSpans {
		resAttrs := attrsToMap(rs.Resource.Attributes)
		resService := resAttrs["service.name"]

		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				if sp.TraceID == "" || sp.SpanID == "" {
					continue
				}

				start := nanoToTime(sp.StartNano)
				end := nanoToTime(sp.EndNano)
				var durMS int64
				if !start.IsZero() && !end.IsZero() {
					durMS = end.Sub(start).Milliseconds()
				}

				spanAttrs := attrsToMap(sp.Attributes)
				attrsJSON, _ := json.Marshal(spanAttrs)

				// Resolve code.filepath → canonical path.
				codeFile := ""
				for _, key := range []string{"code.filepath", "code.namespace"} {
					if raw := spanAttrs[key]; raw != "" {
						if norm, ok := normalizeOTelPath(raw, rootDir); ok {
							codeFile = norm
							break
						}
					}
				}

				lineno := 0
				if v := spanAttrs["code.lineno"]; v != "" {
					if n, err := strconv.Atoi(v); err == nil {
						lineno = n
					}
				}

				spanService := resService
				if sv := spanAttrs["service.name"]; sv != "" {
					spanService = sv
				}

				statusStr := "unset"
				switch sp.Status.Code {
				case 1:
					statusStr = "ok"
				case 2:
					statusStr = "error"
				}

				sd := SpanData{
					SpanID:       sp.SpanID,
					ParentSpanID: sp.ParentSpanID,
					Name:         sp.Name,
					Service:      spanService,
					CodeFile:     codeFile,
					CodeLineno:   lineno,
					CodeFunc:     spanAttrs["code.function"],
					StartedAt:    start,
					DurationMS:   durMS,
					Status:       statusStr,
					ErrorMsg:     sp.Status.Message,
					Attributes:   string(attrsJSON),
				}

				e, exists := byID[sp.TraceID]
				if !exists {
					td := &TraceData{
						TraceID:   sp.TraceID,
						Service:   spanService,
						StartedAt: start,
					}
					e = &entry{td: td}
					byID[sp.TraceID] = e
					order = append(order, sp.TraceID)
				}
				e.td.Spans = append(e.td.Spans, sd)
				if !start.IsZero() && (e.td.StartedAt.IsZero() || start.Before(e.td.StartedAt)) {
					e.td.StartedAt = start
				}
				e.td.DurationMS += durMS
			}
		}
	}

	out := make([]TraceData, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id].td)
	}
	return out, nil
}

// ---- Store.IngestTrace ----

// IngestTrace stores one TraceData (and all its spans) into the CKG store,
// resolving each span's code.filepath:lineno to a CKG node_id at ingest time.
// Existing rows for the same (trace_id, span_id) are replaced atomically.
func (s *Store) IngestTrace(ctx context.Context, td TraceData) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ingest trace: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO traces (id, service, started_at, duration_ms)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			service     = excluded.service,
			started_at  = excluded.started_at,
			duration_ms = excluded.duration_ms`,
		td.TraceID, nullStr(td.Service), td.StartedAt, td.DurationMS)
	if err != nil {
		return fmt.Errorf("ingest trace: upsert trace row: %w", err)
	}

	for i := range td.Spans {
		sp := &td.Spans[i]
		nodeID, resolveStatus := resolveSpanNodeTx(ctx, tx, sp)

		var nodeIDVal interface{}
		if nodeID != 0 {
			nodeIDVal = nodeID
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO spans
				(span_id, trace_id, parent_span_id, name, service,
				 code_file, code_lineno, code_func,
				 ckg_node_id, resolve_status,
				 started_at, duration_ms, status, error_msg, attributes)
			VALUES (?,?,?,?,?, ?,?,?, ?,?, ?,?,?,?,?)
			ON CONFLICT(trace_id, span_id) DO UPDATE SET
				parent_span_id = excluded.parent_span_id,
				name           = excluded.name,
				service        = excluded.service,
				code_file      = excluded.code_file,
				code_lineno    = excluded.code_lineno,
				code_func      = excluded.code_func,
				ckg_node_id    = excluded.ckg_node_id,
				resolve_status = excluded.resolve_status,
				started_at     = excluded.started_at,
				duration_ms    = excluded.duration_ms,
				status         = excluded.status,
				error_msg      = excluded.error_msg,
				attributes     = excluded.attributes`,
			sp.SpanID, td.TraceID, nullStr(sp.ParentSpanID), sp.Name, nullStr(sp.Service),
			nullStr(sp.CodeFile), nullInt(sp.CodeLineno), nullStr(sp.CodeFunc),
			nodeIDVal, resolveStatus,
			sp.StartedAt, sp.DurationMS, sp.Status, nullStr(sp.ErrorMsg), sp.Attributes)
		if err != nil {
			return fmt.Errorf("ingest trace: upsert span %s: %w", sp.SpanID, err)
		}
	}

	return tx.Commit()
}

// resolveSpanNodeTx looks up the innermost CKG node for a span's code location.
// Returns (node_id, resolve_status); node_id is 0 when not resolved.
func resolveSpanNodeTx(ctx context.Context, tx *sql.Tx, sp *SpanData) (int64, string) {
	if sp.CodeFile == "" {
		return 0, ResolveStatusNoCodeAttrs
	}
	if sp.CodeLineno <= 0 {
		return 0, ResolveStatusNoNodeAtLine
	}

	var id int64
	err := tx.QueryRowContext(ctx, `
		SELECT n.id
		FROM nodes n
		JOIN files f ON f.id = n.file_id
		WHERE f.path = ? AND n.line_start <= ? AND n.line_end >= ?
		ORDER BY n.line_start DESC
		LIMIT 1`, sp.CodeFile, sp.CodeLineno, sp.CodeLineno).Scan(&id)
	if err == nil {
		return id, ResolveStatusResolved
	}
	if err != sql.ErrNoRows {
		// Unexpected query error — treat as unresolved.
		return 0, ResolveStatusPathNotInCKG
	}

	// Check whether the file exists in CKG at all.
	var dummy int
	err2 := tx.QueryRowContext(ctx, `SELECT 1 FROM files WHERE path = ? LIMIT 1`, sp.CodeFile).Scan(&dummy)
	if err2 != nil {
		return 0, ResolveStatusPathNotInCKG
	}
	return 0, ResolveStatusNoNodeAtLine
}

// nullStr converts an empty string to nil (SQLite NULL).
func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// nullInt converts 0 to nil (SQLite NULL).
func nullInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}
