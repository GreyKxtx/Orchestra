package ckg

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// ---- normalizeOTelPath ----

func TestNormalizeOTelPath(t *testing.T) {
	root := "/repo/project"
	if runtime.GOOS == "windows" {
		root = `D:\repo\project`
	}

	tests := []struct {
		name    string
		raw     string
		wantRel string
		wantOK  bool
	}{
		{
			name:    "relative slash path",
			raw:     "x/y/file.go",
			wantRel: "x/y/file.go",
			wantOK:  true,
		},
		{
			name:    "relative with ./ prefix",
			raw:     "./x/y/file.go",
			wantRel: "x/y/file.go",
			wantOK:  true,
		},
		{
			name:    "relative backslash",
			raw:     `x\y\file.go`,
			wantRel: "x/y/file.go",
			wantOK:  true,
		},
		{
			name:    "outside rootDir",
			raw:     "../other/file.go",
			wantRel: "",
			wantOK:  false,
		},
		{
			name:    "empty path",
			raw:     "",
			wantRel: "",
			wantOK:  false,
		},
	}

	// Add platform-specific absolute path tests.
	if runtime.GOOS == "windows" {
		tests = append(tests, []struct {
			name    string
			raw     string
			wantRel string
			wantOK  bool
		}{
			{
				name:    "absolute Windows path under root",
				raw:     `D:\repo\project\x\y\file.go`,
				wantRel: "x/y/file.go",
				wantOK:  true,
			},
			{
				name:    "absolute Windows path outside root",
				raw:     `C:\other\file.go`,
				wantRel: "",
				wantOK:  false,
			},
		}...)
	} else {
		tests = append(tests, []struct {
			name    string
			raw     string
			wantRel string
			wantOK  bool
		}{
			{
				name:    "absolute POSIX path under root",
				raw:     "/repo/project/x/y/file.go",
				wantRel: "x/y/file.go",
				wantOK:  true,
			},
			{
				name:    "absolute POSIX path outside root",
				raw:     "/other/file.go",
				wantRel: "",
				wantOK:  false,
			},
		}...)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rel, ok := normalizeOTelPath(tc.raw, root)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (rel=%q)", ok, tc.wantOK, rel)
			}
			if ok && rel != tc.wantRel {
				t.Fatalf("rel = %q, want %q", rel, tc.wantRel)
			}
		})
	}
}

// ---- otlpAttrString ----

func TestOtlpAttrString(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`{"stringValue":"hello"}`, "hello"},
		{`{"intValue":"42"}`, "42"},   // OTLP-JSON: intValue is a string
		{`{"intValue":42}`, "42"},     // fallback: bare number
		{`{"boolValue":true}`, "true"},
		{`{"doubleValue":3.14}`, "3.14"},
		{`{}`, ""},
	}
	for _, tc := range tests {
		got := otlpAttrString(json.RawMessage(tc.raw))
		if got != tc.want {
			t.Errorf("otlpAttrString(%s) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// ---- ParseOTLPJSON ----

func minimalOTLPPayload(traceID, spanID, filePath string, lineno int) []byte {
	return []byte(fmt.Sprintf(`{
		"resourceSpans": [{
			"resource": {"attributes": [
				{"key": "service.name", "value": {"stringValue": "test-service"}}
			]},
			"scopeSpans": [{
				"spans": [{
					"traceId": %q,
					"spanId": %q,
					"name": "handler.do",
					"startTimeUnixNano": "1000000000",
					"endTimeUnixNano": "2000000000",
					"status": {"code": 0},
					"attributes": [
						{"key": "code.filepath", "value": {"stringValue": %q}},
						{"key": "code.lineno",   "value": {"intValue": %q}}
					]
				}]
			}]
		}]
	}`, traceID, spanID, filePath, fmt.Sprintf("%d", lineno)))
}

func TestParseOTLPJSON_Basic(t *testing.T) {
	root := t.TempDir()
	traceID := "aabbccddeeff00112233445566778899"
	spanID := "0011223344556677"
	filePath := "internal/agent/agent.go"

	data := minimalOTLPPayload(traceID, spanID, filePath, 42)
	traces, err := ParseOTLPJSON(data, root)
	if err != nil {
		t.Fatalf("ParseOTLPJSON: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1", len(traces))
	}
	td := traces[0]
	if td.TraceID != traceID {
		t.Errorf("TraceID = %q, want %q", td.TraceID, traceID)
	}
	if td.Service != "test-service" {
		t.Errorf("Service = %q, want test-service", td.Service)
	}
	if len(td.Spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(td.Spans))
	}
	sp := td.Spans[0]
	if sp.CodeFile != filePath {
		t.Errorf("CodeFile = %q, want %q", sp.CodeFile, filePath)
	}
	if sp.CodeLineno != 42 {
		t.Errorf("CodeLineno = %d, want 42", sp.CodeLineno)
	}
	if sp.DurationMS != 1000 {
		t.Errorf("DurationMS = %d, want 1000", sp.DurationMS)
	}
}

func TestParseOTLPJSON_Empty(t *testing.T) {
	traces, err := ParseOTLPJSON([]byte(`{"resourceSpans":[]}`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 0 {
		t.Fatalf("expected 0 traces, got %d", len(traces))
	}
}

// ---- IngestTrace + FindNodeAtLine + RuntimeQuery via Store ----

func TestIngestTrace_ResolveStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Index a file with one function node at lines 5–15.
	nodes := []Node{
		{FQN: "ex/pkg.Handler", ShortName: "Handler", Kind: "func", LineStart: 5, LineEnd: 15},
	}
	if err := s.SaveFileNodes(ctx, "handler.go", "h1", "go", "ex", "pkg", nodes, nil); err != nil {
		t.Fatalf("SaveFileNodes: %v", err)
	}

	td := TraceData{
		TraceID:   "trace0001",
		Service:   "svc",
		StartedAt: time.Now(),
		Spans: []SpanData{
			{SpanID: "span0001", Name: "ok", CodeFile: "handler.go", CodeLineno: 10},
			{SpanID: "span0002", Name: "no-code-attrs"},
			{SpanID: "span0003", Name: "bad-file", CodeFile: "missing.go", CodeLineno: 5},
			{SpanID: "span0004", Name: "bad-line", CodeFile: "handler.go", CodeLineno: 99},
		},
	}

	if err := s.IngestTrace(ctx, td); err != nil {
		t.Fatalf("IngestTrace: %v", err)
	}

	expectStatus := func(t *testing.T, spanID, want string) {
		t.Helper()
		var got string
		err := s.db.QueryRowContext(ctx, `SELECT resolve_status FROM spans WHERE span_id = ?`, spanID).Scan(&got)
		if err != nil {
			t.Fatalf("query span %s: %v", spanID, err)
		}
		if got != want {
			t.Errorf("span %s resolve_status = %q, want %q", spanID, got, want)
		}
	}
	expectNodeID := func(t *testing.T, spanID string, wantNil bool) {
		t.Helper()
		var nodeID *int64
		err := s.db.QueryRowContext(ctx, `SELECT ckg_node_id FROM spans WHERE span_id = ?`, spanID).Scan(&nodeID)
		if err != nil {
			t.Fatalf("query span %s: %v", spanID, err)
		}
		if wantNil && nodeID != nil {
			t.Errorf("span %s: expected NULL node_id, got %d", spanID, *nodeID)
		}
		if !wantNil && nodeID == nil {
			t.Errorf("span %s: expected non-NULL node_id, got NULL", spanID)
		}
	}

	expectStatus(t, "span0001", ResolveStatusResolved)
	expectNodeID(t, "span0001", false)

	expectStatus(t, "span0002", ResolveStatusNoCodeAttrs)
	expectNodeID(t, "span0002", true)

	expectStatus(t, "span0003", ResolveStatusPathNotInCKG)
	expectNodeID(t, "span0003", true)

	expectStatus(t, "span0004", ResolveStatusNoNodeAtLine)
	expectNodeID(t, "span0004", true)
}

// TestIngestTrace_Idempotent verifies that re-ingesting the same trace replaces rows cleanly.
func TestIngestTrace_Idempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	td := TraceData{
		TraceID:   "idempotent-trace",
		Service:   "svc",
		StartedAt: time.Now(),
		Spans: []SpanData{
			{SpanID: "s1", Name: "first"},
		},
	}
	if err := s.IngestTrace(ctx, td); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	td.Spans[0].Name = "updated"
	if err := s.IngestTrace(ctx, td); err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM spans WHERE span_id = 's1'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "updated" {
		t.Errorf("name = %q, want updated", name)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM spans`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("span count = %d, want 1", count)
	}
}

// TestFindNodeAtLine is a unit test for the Store helper.
func TestFindNodeAtLine(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{
		{FQN: "pkg.Outer", ShortName: "Outer", Kind: "func", LineStart: 1, LineEnd: 20},
		{FQN: "pkg.Inner", ShortName: "Inner", Kind: "func", LineStart: 8, LineEnd: 15},
	}
	if err := s.SaveFileNodes(ctx, "f.go", "h", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}

	id, err := s.FindNodeAtLine(ctx, "f.go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id for line 10")
	}
	// Line 10 is inside Inner (8–15) which starts later → innermost wins.
	var fqn string
	if err := s.db.QueryRowContext(ctx, `SELECT fqn FROM nodes WHERE id = ?`, id).Scan(&fqn); err != nil {
		t.Fatal(err)
	}
	if fqn != "pkg.Inner" {
		t.Errorf("innermost node fqn = %q, want pkg.Inner", fqn)
	}

	// Line outside both nodes.
	id2, _ := s.FindNodeAtLine(ctx, "f.go", 99)
	if id2 != 0 {
		t.Errorf("expected 0 for out-of-range line, got %d", id2)
	}

	// Nonexistent file.
	id3, _ := s.FindNodeAtLine(ctx, "nope.go", 5)
	if id3 != 0 {
		t.Errorf("expected 0 for nonexistent file, got %d", id3)
	}
}

