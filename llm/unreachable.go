package llm

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// UnreachableError means the HTTP client never reached the LLM server
// (dial/refused/timeout). Compaction must not be attempted on this error.
type UnreachableError struct {
	Endpoint string
	Err      error
}

func (e *UnreachableError) Error() string {
	ep := strings.TrimSpace(e.Endpoint)
	if ep == "" {
		ep = "(unknown)"
	}
	return fmt.Sprintf("LLM Endpoint unreachable at %s. Check if LM Studio / vLLM is running.", ep)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// IsUnreachableError reports a connect-level failure (dial tcp, connection
// refused, i/o timeout on the initial POST). Mid-stream stalls stay transient.
func IsUnreachableError(err error) bool {
	if err == nil {
		return false
	}
	var u *UnreachableError
	if errors.As(err, &u) {
		return true
	}
	s := strings.ToLower(err.Error())
	connectWrap := strings.Contains(s, "failed to send stream request") ||
		strings.Contains(s, "failed to send request") ||
		strings.Contains(s, "dial tcp") ||
		strings.Contains(s, "connectex")
	if !connectWrap {
		return false
	}
	for _, marker := range []string{
		"connection refused", "dial tcp", "i/o timeout", "no such host",
		"connectex", "network is unreachable", "connection timed out",
		"actively refused", "wsasend", "wsarecv",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return connectWrap
	}
	return false
}

func wrapUnreachable(endpoint string, err error) error {
	if err == nil || !IsUnreachableError(err) {
		return err
	}
	var u *UnreachableError
	if errors.As(err, &u) {
		return err
	}
	return &UnreachableError{Endpoint: endpointDisplay(endpoint), Err: err}
}

func endpointDisplay(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	return baseURL
}
