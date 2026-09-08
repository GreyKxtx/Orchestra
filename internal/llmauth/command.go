package llmauth

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// commandTokenSource runs an external helper that prints a bearer on stdout
// and caches the result for ttl.
//
// The cache is not an optimization: without it a helper like
// `gcloud auth print-access-token` would fork on every single LLM request.
// A failed run is deliberately not cached -- a transient failure must not
// lock the provider out for the rest of the TTL.
type commandTokenSource struct {
	name string
	argv []string
	ttl  time.Duration

	mu      sync.Mutex
	token   string
	expires time.Time
}

// newCommandTokenSource returns a token source backed by an external command.
func newCommandTokenSource(name string, argv []string, ttl time.Duration) func() (string, error) {
	s := &commandTokenSource{name: name, argv: argv, ttl: ttl}
	return s.get
}

func (s *commandTokenSource) get() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.expires) {
		return s.token, nil
	}

	cmd := exec.Command(s.argv[0], s.argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("provider %q: token_command failed: %w", s.name, err)
		}
		return "", fmt.Errorf("provider %q: token_command failed: %w: %s", s.name, err, msg)
	}
	tok := strings.TrimSpace(stdout.String())
	if tok == "" {
		return "", fmt.Errorf("provider %q: token_command printed nothing on stdout", s.name)
	}
	s.token = tok
	s.expires = time.Now().Add(s.ttl)
	return tok, nil
}
