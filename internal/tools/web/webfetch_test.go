package web

import (
	"strings"
	"testing"
)

func TestExtractTextFromHTML(t *testing.T) {
	html := `<html><head><title>My Page</title></head><body>
<script>alert('skip me')</script>
<p>Hello world</p>
<p>Second paragraph</p>
</body></html>`

	title, content := extractTextFromHTML(html)
	if title != "My Page" {
		t.Fatalf("expected title 'My Page', got %q", title)
	}
	if !strings.Contains(content, "Hello world") {
		t.Fatalf("content should contain 'Hello world', got: %q", content)
	}
	if strings.Contains(content, "alert") {
		t.Fatalf("script content should be stripped, got: %q", content)
	}
}

func TestIsLikelyHTML(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`<html><body>hi</body></html>`, true},
		{`<!DOCTYPE html><html>`, true},
		{`<head><title>t</title></head>`, true},
		{`<body>content</body>`, true},
		{`{"json": true}`, false},
		{`plain text content`, false},
	}
	for _, tc := range cases {
		got := isLikelyHTML([]byte(tc.input))
		if got != tc.want {
			t.Errorf("isLikelyHTML(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
