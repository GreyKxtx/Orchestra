package ops

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---- AnyOp.UnmarshalJSON ----

func TestAnyOp_UnmarshalJSON_ThreeTypes(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantOp  string
		checkFn func(t *testing.T, got AnyOp)
	}{
		{
			name:   "replace_range",
			json:   `{"op":"file.replace_range","path":"a.go","range":{"start":{"line":0,"col":0},"end":{"line":1,"col":0}},"expected":"old","replacement":"new"}`,
			wantOp: OpFileReplaceRange,
			checkFn: func(t *testing.T, got AnyOp) {
				if got.ReplaceRange == nil {
					t.Fatal("ReplaceRange is nil")
				}
				if got.WriteAtomic != nil || got.MkdirAll != nil {
					t.Error("unexpected non-nil sibling ops")
				}
				if got.ReplaceRange.Expected != "old" {
					t.Errorf("expected 'old', got %q", got.ReplaceRange.Expected)
				}
			},
		},
		{
			name:   "write_atomic",
			json:   `{"op":"file.write_atomic","path":"b.go","content":"hello"}`,
			wantOp: OpFileWriteAtomic,
			checkFn: func(t *testing.T, got AnyOp) {
				if got.WriteAtomic == nil {
					t.Fatal("WriteAtomic is nil")
				}
				if got.ReplaceRange != nil || got.MkdirAll != nil {
					t.Error("unexpected non-nil sibling ops")
				}
				if got.WriteAtomic.Content != "hello" {
					t.Errorf("content: got %q", got.WriteAtomic.Content)
				}
			},
		},
		{
			name:   "mkdir_all",
			json:   `{"op":"file.mkdir_all","path":"pkg/sub"}`,
			wantOp: OpFileMkdirAll,
			checkFn: func(t *testing.T, got AnyOp) {
				if got.MkdirAll == nil {
					t.Fatal("MkdirAll is nil")
				}
				if got.ReplaceRange != nil || got.WriteAtomic != nil {
					t.Error("unexpected non-nil sibling ops")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got AnyOp
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Op != tc.wantOp {
				t.Errorf("Op: got %q, want %q", got.Op, tc.wantOp)
			}
			tc.checkFn(t, got)
		})
	}
}

func TestAnyOp_UnmarshalJSON_TypeNormalization(t *testing.T) {
	// LLM may emit "type" instead of "op" — should be accepted for all three ops.
	cases := []struct {
		name string
		json string
	}{
		{"replace_range", `{"type":"file.replace_range","path":"a.go","range":{"start":{"line":0,"col":0},"end":{"line":0,"col":0}},"expected":"x","replacement":"y"}`},
		{"write_atomic", `{"type":"file.write_atomic","path":"b.go","content":"hi"}`},
		{"mkdir_all", `{"type":"file.mkdir_all","path":"d"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got AnyOp
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("type normalization failed: %v", err)
			}
			if got.Op == "" {
				t.Error("Op should be set after type normalization")
			}
		})
	}
}

func TestAnyOp_UnmarshalJSON_OpWinsOverType(t *testing.T) {
	// When both "op" and "type" are present, "op" wins.
	j := `{"op":"file.write_atomic","type":"file.replace_range","path":"x.go","content":"c"}`
	var got AnyOp
	if err := json.Unmarshal([]byte(j), &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Op != OpFileWriteAtomic {
		t.Errorf("expected op to win, got %q", got.Op)
	}
	if got.WriteAtomic == nil {
		t.Error("WriteAtomic should be populated when op wins")
	}
}

func TestAnyOp_UnmarshalJSON_Errors(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "missing op",
			json:    `{"path":"a.go"}`,
			wantErr: "missing op",
		},
		{
			name:    "empty op",
			json:    `{"op":"","path":"a.go"}`,
			wantErr: "missing op",
		},
		{
			name:    "unsupported op",
			json:    `{"op":"file.delete","path":"a.go"}`,
			wantErr: "unsupported op",
		},
		{
			name:    "unknown field in write_atomic",
			json:    `{"op":"file.write_atomic","path":"b.go","content":"x","unknown_field":true}`,
			wantErr: "unknown field",
		},
		{
			name:    "unknown field in mkdir_all",
			json:    `{"op":"file.mkdir_all","path":"d","extra":1}`,
			wantErr: "unknown field",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got AnyOp
			err := json.Unmarshal([]byte(tc.json), &got)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestAnyOp_UnmarshalJSON_UnknownFieldInReplaceRange(t *testing.T) {
	// NOTE: ReplaceRangeOp.UnmarshalJSON uses standard json.Unmarshal (not strict)
	// so unknown fields are silently ignored — asymmetric vs write_atomic/mkdir_all.
	// This test documents the current (permissive) behavior, not a desired invariant.
	j := `{"op":"file.replace_range","path":"a.go","range":{"start":{"line":0,"col":0},"end":{"line":0,"col":0}},"expected":"x","replacement":"y","unknown_field":true}`
	var got AnyOp
	if err := json.Unmarshal([]byte(j), &got); err != nil {
		t.Errorf("replace_range currently accepts unknown fields (permissive); unexpected error: %v", err)
	}
}

// ---- ReplaceRangeOp.UnmarshalJSON ----

func TestReplaceRangeOp_TypeNormalization(t *testing.T) {
	j := `{"type":"file.replace_range","path":"a.go","range":{"start":{"line":2,"col":4},"end":{"line":3,"col":0}},"expected":"foo","replacement":"bar"}`
	var got ReplaceRangeOp
	if err := json.Unmarshal([]byte(j), &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Op != OpFileReplaceRange {
		t.Errorf("Op: got %q, want %q", got.Op, OpFileReplaceRange)
	}
	if got.Range.Start.Line != 2 || got.Range.Start.Col != 4 {
		t.Errorf("Start: got %+v", got.Range.Start)
	}
	if got.Expected != "foo" || got.Replacement != "bar" {
		t.Errorf("Expected/Replacement mismatch")
	}
}

// ---- AnyOp.MarshalJSON ----

func TestAnyOp_MarshalJSON_RoundTrip(t *testing.T) {
	cases := []string{
		`{"op":"file.replace_range","path":"a.go","range":{"start":{"line":0,"col":0},"end":{"line":1,"col":0}},"expected":"old","replacement":"new","conditions":{}}`,
		`{"op":"file.write_atomic","path":"b.go","content":"hello","conditions":{}}`,
		`{"op":"file.mkdir_all","path":"pkg/sub"}`,
	}
	for _, orig := range cases {
		var a AnyOp
		if err := json.Unmarshal([]byte(orig), &a); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var a2 AnyOp
		if err := json.Unmarshal(b, &a2); err != nil {
			t.Fatalf("re-unmarshal: %v", err)
		}
		if a2.Op != a.Op || a2.Path != a.Path {
			t.Errorf("round-trip mismatch: op %q→%q path %q→%q", a.Op, a2.Op, a.Path, a2.Path)
		}
	}
}

func TestAnyOp_MarshalJSON_Fallback(t *testing.T) {
	a := AnyOp{Op: "file.unknown", Path: "x.go"}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if m["op"] != "file.unknown" {
		t.Errorf("op: got %v", m["op"])
	}
	if m["path"] != "x.go" {
		t.Errorf("path: got %v", m["path"])
	}
}

// ---- WrapReplaceRangeOps ----

func TestWrapReplaceRangeOps_Empty(t *testing.T) {
	out := WrapReplaceRangeOps(nil)
	if out == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(out) != 0 {
		t.Errorf("expected len 0, got %d", len(out))
	}
}

func TestWrapReplaceRangeOps_NoAliasing(t *testing.T) {
	in := []ReplaceRangeOp{
		{Op: OpFileReplaceRange, Path: "a.go", Replacement: "original"},
		{Op: OpFileReplaceRange, Path: "b.go", Replacement: "second"},
	}
	out := WrapReplaceRangeOps(in)
	// Mutate original — out should be unaffected.
	in[0].Replacement = "mutated"
	if out[0].ReplaceRange.Replacement != "original" {
		t.Errorf("aliasing: mutation of input affected output: got %q", out[0].ReplaceRange.Replacement)
	}
}

func TestWrapReplaceRangeOps_AllWrapped(t *testing.T) {
	in := []ReplaceRangeOp{
		{Op: OpFileReplaceRange, Path: "a.go"},
		{Op: OpFileReplaceRange, Path: "b.go"},
	}
	out := WrapReplaceRangeOps(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(out))
	}
	for i, o := range out {
		if o.ReplaceRange == nil {
			t.Errorf("out[%d].ReplaceRange is nil", i)
		}
		if o.Op != OpFileReplaceRange {
			t.Errorf("out[%d].Op = %q", i, o.Op)
		}
	}
}
