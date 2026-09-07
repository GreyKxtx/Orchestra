package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/permission"
)

func consentWith(t *testing.T, typed string) (permission.Response, string) {
	t.Helper()
	var out bytes.Buffer
	c := &terminalConsent{in: strings.NewReader(typed), out: &out}
	resp, err := c.RequestPermission(context.Background(), permission.Request{
		Tool: "mcp:linear", Kind: "mcp.sampling", Description: "2 message(s): summarise", Reason: "wants your model",
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp, out.String()
}

func TestTerminalConsent_PromptNamesTheServer(t *testing.T) {
	_, shown := consentWith(t, "n\n")
	for _, want := range []string{"linear", "summarise", "[y/n/a]"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("prompt missing %q:\n%s", want, shown)
		}
	}
}

func TestTerminalConsent_Answers(t *testing.T) {
	cases := []struct {
		typed    string
		approved bool
		always   bool
	}{
		{"y\n", true, false},
		{"Y\n", true, false},
		{"a\n", true, true},
		{"n\n", false, false},
		{"\n", false, false},     // Enter alone is a refusal
		{"", false, false},       // EOF: nobody answered
		{"what\n", false, false}, // anything else is not a yes
	}
	for _, c := range cases {
		resp, _ := consentWith(t, c.typed)
		if resp.Approved != c.approved || resp.Always != c.always {
			t.Errorf("typed %q → %+v, want approved=%v always=%v", c.typed, resp, c.approved, c.always)
		}
	}
}
