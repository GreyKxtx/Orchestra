package mcp

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseCallResult_TextOnly(t *testing.T) {
	text, images, isErr, err := parseCallResult([]byte(`{"content":[{"type":"text","text":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" || isErr {
		t.Errorf("text=%q isErr=%v", text, isErr)
	}
	if len(images) != 0 {
		t.Errorf("images = %v, want none", images)
	}
	if strings.Contains(text, "dropped") {
		t.Error("a text-only result must not carry a drop notice")
	}
}

func TestParseCallResult_ImageIsCarriedNotDropped(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG-bytes"))
	raw := []byte(`{"content":[
		{"type":"text","text":"here is the page"},
		{"type":"image","data":"` + png + `","mimeType":"image/png"}
	]}`)

	text, images, _, err := parseCallResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != "here is the page" {
		t.Errorf("text = %q", text)
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want 1", len(images))
	}
	if images[0].MIME != "image/png" || string(images[0].Data) != "\x89PNG-bytes" {
		t.Errorf("image = %+v", images[0])
	}
	if strings.Contains(text, "dropped") {
		t.Error("an image that is forwarded must not be reported as dropped")
	}
}

func TestParseCallResult_StillReportsWhatItCannotCarry(t *testing.T) {
	raw := []byte(`{"content":[
		{"type":"text","text":"ok"},
		{"type":"audio","data":"AAA","mimeType":"audio/wav"}
	]}`)
	text, images, _, err := parseCallResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if !strings.Contains(text, "dropped 1") {
		t.Errorf("text = %q, want a drop notice for the audio item", text)
	}
}

func TestParseCallResult_UndecodableImageIsADropNotAPanic(t *testing.T) {
	raw := []byte(`{"content":[{"type":"image","data":"!!!not base64!!!","mimeType":"image/png"}]}`)
	text, images, _, err := parseCallResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %d, want 0", len(images))
	}
	if !strings.Contains(text, "dropped 1") {
		t.Errorf("text = %q, want the undecodable image counted as dropped", text)
	}
}

func TestParseCallResult_ErrorFlag(t *testing.T) {
	text, _, isErr, err := parseCallResult([]byte(`{"content":[{"type":"text","text":"boom"}],"isError":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !isErr || text != "boom" {
		t.Errorf("isErr=%v text=%q", isErr, text)
	}
}
