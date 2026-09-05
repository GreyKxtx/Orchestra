package mcp

import (
	"strings"
	"testing"
)

func TestNegotiateProtocolVersion(t *testing.T) {
	cases := []struct {
		server string
		want   string
		known  bool
	}{
		// The server agreeing with us is the happy path.
		{mcpProtocolVersion, mcpProtocolVersion, true},
		// A server that only speaks the older revision answers with it; the
		// whole point of negotiation is that this keeps working.
		{"2024-11-05", "2024-11-05", true},
		{"2025-03-26", "2025-03-26", true},
		// A server ahead of us: proceed, but say we do not recognise it. A
		// server is far more likely to be newer than broken.
		{"2099-01-01", "2099-01-01", false},
		// No version at all: assume the revision that predates the field.
		{"", "2024-11-05", true},
	}
	for _, tc := range cases {
		got, known := negotiateProtocolVersion(tc.server)
		if got != tc.want || known != tc.known {
			t.Errorf("negotiateProtocolVersion(%q) = %q/%v, want %q/%v", tc.server, got, known, tc.want, tc.known)
		}
	}
}

func TestClientRequestsTheCurrentRevision(t *testing.T) {
	if mcpProtocolVersion != "2025-06-18" {
		t.Errorf("client offers %q; the current revision is 2025-06-18", mcpProtocolVersion)
	}
}

func TestParseCallResult_StructuredContentFillsInForAToolWithNoText(t *testing.T) {
	// 2025-06-18 lets a tool answer with structuredContent and a text mirror.
	// Servers that send only the structured half used to reach the model as
	// an empty result.
	text, _, _, err := parseCallResult([]byte(`{"content":[],"structuredContent":{"status":"ok","count":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"count":3`) {
		t.Errorf("text = %q, want the structured payload", text)
	}
}

func TestParseCallResult_TextWinsOverStructuredContent(t *testing.T) {
	// When both halves are present the text is the one written for a reader;
	// sending both would just duplicate the answer in the context window.
	text, _, _, err := parseCallResult([]byte(`{"content":[{"type":"text","text":"3 items"}],"structuredContent":{"count":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "3 items" {
		t.Errorf("text = %q, want only the text content", text)
	}
}
