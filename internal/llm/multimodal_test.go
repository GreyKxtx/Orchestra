package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageMarshal_TextOnly_UsesStringContent(t *testing.T) {
	m := Message{Role: RoleUser, Content: "hello"}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"content":"hello"`) {
		t.Errorf("expected string content: %s", s)
	}
	// Ensure no array form for text-only messages.
	if strings.Contains(s, `"type":"text"`) {
		t.Errorf("unexpected multimodal form: %s", s)
	}
}

func TestMessageMarshal_WithPartsTextAndImageData(t *testing.T) {
	m := Message{
		Role: RoleUser,
		Parts: []ContentPart{
			{Kind: PartText, Text: "what is this?"},
			{Kind: PartImage, ImageData: []byte{0x89, 0x50, 0x4E, 0x47}, ImageMIME: "image/png"},
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"role":"user"`) {
		t.Errorf("role: %s", s)
	}
	if !strings.Contains(s, `"type":"text"`) || !strings.Contains(s, `"what is this?"`) {
		t.Errorf("text part missing: %s", s)
	}
	if !strings.Contains(s, `"type":"image_url"`) {
		t.Errorf("image part missing: %s", s)
	}
	if !strings.Contains(s, `"data:image/png;base64,`) {
		t.Errorf("data URI missing: %s", s)
	}
}

func TestMessageMarshal_WithPartsImageURL(t *testing.T) {
	m := Message{
		Role: RoleUser,
		Parts: []ContentPart{
			{Kind: PartImage, ImageURL: "https://example.com/x.png"},
		},
	}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"url":"https://example.com/x.png"`) {
		t.Errorf("URL part missing: %s", b)
	}
}

func TestMessage_TextLen(t *testing.T) {
	m1 := Message{Content: "abc"}
	if m1.TextLen() != 3 {
		t.Errorf("string content len: %d", m1.TextLen())
	}
	m2 := Message{Parts: []ContentPart{
		{Kind: PartText, Text: "abc"},
		{Kind: PartImage, ImageData: make([]byte, 1000000)},
		{Kind: PartText, Text: "de"},
	}}
	if m2.TextLen() != 5 {
		t.Errorf("multimodal text len: %d (image bytes should not count)", m2.TextLen())
	}
}

func TestMessage_HasImages(t *testing.T) {
	if (Message{Content: "x"}).HasImages() {
		t.Error("string-only should not have images")
	}
	if !(Message{Parts: []ContentPart{{Kind: PartImage, ImageURL: "x"}}}).HasImages() {
		t.Error("expected HasImages true")
	}
}

func TestMessageUnmarshal_RoundTripMultimodal(t *testing.T) {
	orig := Message{
		Role: RoleUser,
		Parts: []ContentPart{
			{Kind: PartText, Text: "describe"},
			{Kind: PartImage, ImageData: []byte{0x89, 0x50, 0x4E, 0x47}, ImageMIME: "image/png"},
		},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Parts) != 2 {
		t.Fatalf("parts len = %d", len(got.Parts))
	}
	if got.Parts[0].Text != "describe" {
		t.Errorf("text part: %q", got.Parts[0].Text)
	}
	if !got.HasImages() || len(got.Parts[1].ImageData) != 4 {
		t.Errorf("image part not restored: %+v", got.Parts[1])
	}
}

func TestMessageMarshal_PartImageEmptyDataAndURL_Skipped(t *testing.T) {
	m := Message{
		Role: RoleUser,
		Parts: []ContentPart{
			{Kind: PartText, Text: "hi"},
			{Kind: PartImage}, // empty — should be skipped
		},
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), `"image_url"`) {
		t.Errorf("empty image part should be skipped: %s", b)
	}
}
