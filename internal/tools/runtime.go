package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// RuntimeQueryRequest is the input for the runtime.query tool.
type RuntimeQueryRequest struct {
	TraceID string `json:"trace_id"`
	Limit   int    `json:"limit,omitempty"`
}

// RuntimeSpanResult is one span returned by the runtime.query tool.
type RuntimeSpanResult struct {
	SpanID        string    `json:"span_id"`
	ParentSpanID  string    `json:"parent_span_id,omitempty"`
	Name          string    `json:"name"`
	Service       string    `json:"service,omitempty"`
	Status        string    `json:"status"`
	DurationMS    int64     `json:"duration_ms"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	CodeFile      string    `json:"code_file,omitempty"`
	CodeLineno    int       `json:"code_lineno,omitempty"`
	CodeFunc      string    `json:"code_func,omitempty"`
	ResolveStatus string    `json:"resolve_status"`
	// CKG node info — present only when resolve_status == "resolved".
	NodeFQN  string `json:"node_fqn,omitempty"`
	NodeKind string `json:"node_kind,omitempty"`
	ErrorMsg string `json:"error_msg,omitempty"`
	// Raw attributes JSON blob.
	Attributes json.RawMessage `json:"attributes,omitempty"`
}

// RuntimeQueryResponse is returned by the runtime.query tool.
type RuntimeQueryResponse struct {
	TraceID string              `json:"trace_id"`
	Service string              `json:"service,omitempty"`
	Spans   []RuntimeSpanResult `json:"spans"`
}

// RuntimeQuery executes the runtime.query tool against the Runner's CKG cache.
func (r *Runner) RuntimeQuery(ctx context.Context, req RuntimeQueryRequest) (*RuntimeQueryResponse, error) {
	if r.ckgStore == nil {
		return nil, fmt.Errorf("runtime.query: CKG store not initialised")
	}
	if req.TraceID == "" {
		return nil, fmt.Errorf("runtime.query: trace_id is required")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 500
	}

	db := r.ckgStore.DB()

	// Fetch trace metadata.
	var traceService sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT service FROM traces WHERE id = ?`, req.TraceID).Scan(&traceService)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("runtime.query: trace %q not found", req.TraceID)
	}
	if err != nil {
		return nil, fmt.Errorf("runtime.query: fetch trace: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			s.span_id, s.parent_span_id, s.name, s.service,
			s.status, s.duration_ms, s.started_at,
			s.code_file, s.code_lineno, s.code_func,
			s.resolve_status, s.error_msg, s.attributes,
			n.fqn, n.kind
		FROM spans s
		LEFT JOIN nodes n ON n.id = s.ckg_node_id
		WHERE s.trace_id = ?
		ORDER BY s.started_at
		LIMIT ?`, req.TraceID, limit)
	if err != nil {
		return nil, fmt.Errorf("runtime.query: fetch spans: %w", err)
	}
	defer rows.Close()

	var spans []RuntimeSpanResult
	for rows.Next() {
		var (
			spanID, name                                    string
			parentSpanID, service, status                   sql.NullString
			durationMS                                      sql.NullInt64
			startedAt                                       sql.NullTime
			codeFile, codeFunc, resolveStatus, errorMsg    sql.NullString
			codeLineno                                      sql.NullInt64
			attrsRaw                                        sql.NullString
			nodeFQN, nodeKind                               sql.NullString
		)
		err := rows.Scan(
			&spanID, &parentSpanID, &name, &service,
			&status, &durationMS, &startedAt,
			&codeFile, &codeLineno, &codeFunc,
			&resolveStatus, &errorMsg, &attrsRaw,
			&nodeFQN, &nodeKind,
		)
		if err != nil {
			return nil, fmt.Errorf("runtime.query: scan span: %w", err)
		}

		sr := RuntimeSpanResult{
			SpanID:        spanID,
			ParentSpanID:  parentSpanID.String,
			Name:          name,
			Service:       service.String,
			Status:        status.String,
			DurationMS:    durationMS.Int64,
			CodeFile:      codeFile.String,
			CodeLineno:    int(codeLineno.Int64),
			CodeFunc:      codeFunc.String,
			ResolveStatus: resolveStatus.String,
			ErrorMsg:      errorMsg.String,
			NodeFQN:       nodeFQN.String,
			NodeKind:      nodeKind.String,
		}
		if startedAt.Valid {
			sr.StartedAt = startedAt.Time
		}
		if attrsRaw.Valid && attrsRaw.String != "" {
			sr.Attributes = json.RawMessage(attrsRaw.String)
		}

		spans = append(spans, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtime.query: iterate spans: %w", err)
	}

	return &RuntimeQueryResponse{
		TraceID: req.TraceID,
		Service: traceService.String,
		Spans:   spans,
	}, nil
}
