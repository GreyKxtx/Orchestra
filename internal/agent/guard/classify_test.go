package guard

import (
	"fmt"
	"testing"

	"github.com/orchestra/orchestra/internal/protocol"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		hint ErrorKind
		want ErrorKind
	}{
		{protocol.NewError(protocol.StaleContent, "hash", nil), ErrorKindNone, ErrorKindApplyRecoverable},
		{protocol.NewError(protocol.AmbiguousMatch, "multi", nil), ErrorKindResolveFailed, ErrorKindApplyRecoverable},
		{protocol.NewError(protocol.ExecDenied, "no", nil), ErrorKindNone, ErrorKindDenied},
		{protocol.NewError(protocol.InvalidLLMOutput, "search is empty", nil), ErrorKindResolveFailed, ErrorKindResolveFailed},
		{nil, ErrorKindInvalid, ErrorKindInvalid},
		{nil, ErrorKindToolError, ErrorKindToolError},
		{fmt.Errorf("something failed"), ErrorKindNone, ErrorKindToolError},
		{protocol.NewError(protocol.InvalidLLMOutput, "bad json", nil), ErrorKindNone, ErrorKindInvalid},
	}
	for _, tc := range cases {
		got := Classify(tc.err, tc.hint)
		if got != tc.want {
			t.Errorf("Classify(%v, %v)=%v want %v", tc.err, tc.hint, got, tc.want)
		}
	}
}

func TestErrorKindString(t *testing.T) {
	if ErrorKindInvalid.String() != "validation_error" {
		t.Fatal(ErrorKindInvalid.String())
	}
	if ErrorKindApplyRecoverable.String() != "apply_recoverable" {
		t.Fatal(ErrorKindApplyRecoverable.String())
	}
}

func TestCircuitBreaker_RecordClassifiesAndObserves(t *testing.T) {
	cb := NewCircuitBreaker(2, 6, 6, 3)
	var seen []string
	cb.SetOnClassified(func(kind ErrorKind, meta RecordMeta) {
		seen = append(seen, kind.String())
	})
	if err := cb.Record(ErrorKindInvalid, RecordMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := cb.RecordResolveFailure(fmt.Errorf("no match")); err != nil {
		t.Fatal(err)
	}
	if err := cb.RecordApplyRecoverable(protocol.NewError(protocol.StaleContent, "x", nil)); err != nil {
		t.Fatal(err)
	}
	want := []string{"validation_error", "resolve_failed", "apply_recoverable"}
	if len(seen) != len(want) {
		t.Fatalf("seen=%v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("seen[%d]=%q want %q", i, seen[i], want[i])
		}
	}
}
