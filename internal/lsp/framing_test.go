package lsp_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp"
)

func TestReadWriteMessage_RoundTrip(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	var buf bytes.Buffer
	if err := lsp.WriteMessage(&buf, body); err != nil {
		t.Fatal(err)
	}
	got, err := lsp.ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q, want %q", got, body)
	}
}

func TestReadWriteMessage_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := lsp.WriteMessage(&buf, []byte{}); err != nil {
		t.Fatal(err)
	}
	got, err := lsp.ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty body, got %q", got)
	}
}

func TestReadWriteMessage_WithNewlines(t *testing.T) {
	body := []byte("line1\nline2\r\nline3")
	var buf bytes.Buffer
	if err := lsp.WriteMessage(&buf, body); err != nil {
		t.Fatal(err)
	}
	got, err := lsp.ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q, want %q", got, body)
	}
}

func TestReadWriteMessage_Multiple(t *testing.T) {
	msgs := [][]byte{
		[]byte(`{"id":1}`),
		[]byte(`{"id":2}`),
		[]byte(`{"id":3}`),
	}
	var buf bytes.Buffer
	for _, m := range msgs {
		if err := lsp.WriteMessage(&buf, m); err != nil {
			t.Fatal(err)
		}
	}
	r := bufio.NewReader(&buf)
	for i, want := range msgs {
		got, err := lsp.ReadMessage(r)
		if err != nil {
			t.Fatalf("msg %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("msg %d: got %q, want %q", i, got, want)
		}
	}
}
