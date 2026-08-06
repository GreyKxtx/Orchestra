// ui/tui/mention_test.go
package tui

import "testing"

func TestMentionQuery(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello @internal/resolver", "internal/resolver"},
		{"@src", "src"},
		{"no at sign", ""},
		{"text @", ""},
		{"@", ""},
		{"double @foo @bar", "bar"},
		// completed mention — no active query
		{"look @src/main.go ", ""},
		{"look @src/main.go and more", ""},
		{"user@host", ""},
	}
	for _, tc := range cases {
		got := mentionQuery(tc.input)
		if got != tc.want {
			t.Errorf("mentionQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestActiveMentionQuery(t *testing.T) {
	cases := []struct {
		input  string
		query  string
		active bool
	}{
		{"@", "", true},
		{"hello @", "", true},
		{"hello @fi", "fi", true},
		{"hello @src/main.go ", "", false},
		{"hello world", "", false},
		{"user@host", "", false},
		{"see @a.go and", "", false},
	}
	for _, tc := range cases {
		q, active := activeMentionQuery(tc.input)
		if q != tc.query || active != tc.active {
			t.Errorf("activeMentionQuery(%q) = (%q,%v), want (%q,%v)",
				tc.input, q, active, tc.query, tc.active)
		}
	}
}

func TestReplaceLastMention(t *testing.T) {
	cases := []struct {
		input       string
		replacement string
		want        string
	}{
		{"fix @int", "internal/resolver.go", "fix @internal/resolver.go "},
		{"@src", "src/main.go", "@src/main.go "},
		{"hello @foo bar", "baz.go", "hello @baz.go bar"},
		{"no at", "x", "no at"},
		{"@x", "@already.go", "@already.go "},
	}
	for _, tc := range cases {
		got := replaceLastMention(tc.input, tc.replacement)
		if got != tc.want {
			t.Errorf("replaceLastMention(%q, %q) = %q, want %q", tc.input, tc.replacement, got, tc.want)
		}
	}
}
