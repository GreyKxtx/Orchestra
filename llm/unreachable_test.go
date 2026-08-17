package llm

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsUnreachableError(t *testing.T) {
	dial := fmt.Errorf(`failed to send stream request: Post "http://10.5.0.2:1234/v1/chat/completions": dial tcp 10.5.0.2:1234: i/o timeout`)
	if !IsUnreachableError(dial) {
		t.Fatal("dial timeout should be unreachable")
	}
	refused := errors.New(`failed to send stream request: dial tcp 127.0.0.1:1234: connect: connection refused`)
	if !IsUnreachableError(refused) {
		t.Fatal("connection refused should be unreachable")
	}
	wrapped := wrapUnreachable("http://127.0.0.1:1234/v1", dial)
	var u *UnreachableError
	if !errors.As(wrapped, &u) {
		t.Fatalf("wrap: %v", wrapped)
	}
	if !strings.Contains(u.Error(), "LLM Endpoint unreachable at http://127.0.0.1:1234") {
		t.Fatalf("message = %q", u.Error())
	}
	if IsUnreachableError(errors.New("request failed (status 400): context length")) {
		t.Fatal("overflow must not be unreachable")
	}
	if IsTransientLLMError(dial) {
		t.Fatal("unreachable must not be retried as transient")
	}
}
