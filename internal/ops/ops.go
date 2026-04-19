package ops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Internal Ops v1

const (
	OpFileReplaceRange = "file.replace_range"
	OpFileWriteAtomic  = "file.write_atomic"
	OpFileMkdirAll     = "file.mkdir_all"
)

type Position struct {
	Line int `json:"line"` // 0-based
	Col  int `json:"col"`  // 0-based (UTF-8 byte offset within the line)
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Conditions struct {
	FileHash    string `json:"file_hash,omitempty"`   // sha256:<hex>
	AllowFuzzy  bool   `json:"allow_fuzzy,omitempty"` // default: false
	FuzzyWindow int    `json:"fuzzy_window,omitempty"`
}

// ReplaceRangeOp replaces an exact expected slice (strict) with a replacement.
//
// Coordinates use 0-based line/col and an end-exclusive range.
type ReplaceRangeOp struct {
	Op          string     `json:"op"`
	Path        string     `json:"path"`
	Range       Range      `json:"range"`
	Expected    string     `json:"expected"`
	Replacement string     `json:"replacement"`
	Conditions  Conditions `json:"conditions,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling to normalize common LLM mistakes.
// Specifically, normalizes "type" field to "op" if "op" is missing.
// This handles cases where LLM generates {"type": "file.replace_range"} instead of {"op": "file.replace_range"}.
func (r *ReplaceRangeOp) UnmarshalJSON(data []byte) error {
	// First, decode into a map to check for "type" field
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Normalize: if "op" is missing but "type" exists, use "type" as "op"
	if _, hasOp := raw["op"]; !hasOp {
		if typeVal, hasType := raw["type"]; hasType {
			raw["op"] = typeVal
			// Re-encode with normalized field
			data, _ = json.Marshal(raw)
		}
	}

	// Use type alias to avoid infinite recursion
	type Alias ReplaceRangeOp
	var alias Alias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*r = ReplaceRangeOp(alias)
	return nil
}

type WriteAtomicConditions struct {
	MustNotExist bool   `json:"must_not_exist,omitempty"`
	FileHash     string `json:"file_hash,omitempty"` // sha256:<hex>
}

// WriteAtomicOp writes full file content atomically (create or replace).
type WriteAtomicOp struct {
	Op         string                `json:"op"`
	Path       string                `json:"path"`
	Content    string                `json:"content"`
	Mode       int                   `json:"mode,omitempty"` // e.g. 420 = 0644
	Conditions WriteAtomicConditions `json:"conditions,omitempty"`
}

// MkdirAllOp creates a directory path inside the workspace.
type MkdirAllOp struct {
	Op   string `json:"op"`
	Path string `json:"path"`
	Mode int    `json:"mode,omitempty"` // e.g. 493 = 0755
}

// AnyOp is a union of supported internal ops for fs.apply_ops.
//
// It keeps the raw "op" string for stable JSON and provides typed accessors.
type AnyOp struct {
	Op   string `json:"op"`
	Path string `json:"path,omitempty"`

	ReplaceRange *ReplaceRangeOp `json:"-"`
	WriteAtomic  *WriteAtomicOp  `json:"-"`
	MkdirAll     *MkdirAllOp     `json:"-"`
}

func (o AnyOp) MarshalJSON() ([]byte, error) {
	switch {
	case o.ReplaceRange != nil:
		return json.Marshal(o.ReplaceRange)
	case o.WriteAtomic != nil:
		return json.Marshal(o.WriteAtomic)
	case o.MkdirAll != nil:
		return json.Marshal(o.MkdirAll)
	default:
		// Fallback: emit op/path only.
		type minimal struct {
			Op   string `json:"op"`
			Path string `json:"path,omitempty"`
		}
		return json.Marshal(minimal{Op: o.Op, Path: o.Path})
	}
}

func (o *AnyOp) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Normalize: allow "type" as alias for "op".
	if _, hasOp := raw["op"]; !hasOp {
		if tv, ok := raw["type"]; ok {
			raw["op"] = tv
			delete(raw, "type")
			b, err := json.Marshal(raw)
			if err == nil {
				data = b
			}
		}
	}

	var opName string
	if v, ok := raw["op"]; ok {
		_ = json.Unmarshal(v, &opName)
	}
	opName = strings.TrimSpace(opName)
	if opName == "" {
		return fmt.Errorf("missing op")
	}

	switch opName {
	case OpFileReplaceRange:
		var rr ReplaceRangeOp
		if err := strictUnmarshal(data, &rr); err != nil {
			return err
		}
		*o = AnyOp{Op: rr.Op, Path: rr.Path, ReplaceRange: &rr}
		return nil

	case OpFileWriteAtomic:
		var wa WriteAtomicOp
		if err := strictUnmarshal(data, &wa); err != nil {
			return err
		}
		*o = AnyOp{Op: wa.Op, Path: wa.Path, WriteAtomic: &wa}
		return nil

	case OpFileMkdirAll:
		var md MkdirAllOp
		if err := strictUnmarshal(data, &md); err != nil {
			return err
		}
		*o = AnyOp{Op: md.Op, Path: md.Path, MkdirAll: &md}
		return nil

	default:
		return fmt.Errorf("unsupported op: %s", opName)
	}
}

func strictUnmarshal(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON")
		}
		return err
	}
	return nil
}

func WrapReplaceRangeOps(in []ReplaceRangeOp) []AnyOp {
	out := make([]AnyOp, 0, len(in))
	for i := range in {
		rr := in[i]
		rrCopy := rr
		out = append(out, AnyOp{Op: rrCopy.Op, Path: rrCopy.Path, ReplaceRange: &rrCopy})
	}
	return out
}
