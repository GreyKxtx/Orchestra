package format

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

func TestExtractScreenshotImagePart_Base64(t *testing.T) {
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	b64 := base64.StdEncoding.EncodeToString(pngHeader)
	resp, _ := json.Marshal(map[string]any{"image": b64})

	part, ok := ExtractScreenshotImagePart(resp)
	if !ok {
		t.Fatal("expected ok")
	}
	if part.Kind != llm.PartImage {
		t.Errorf("kind: %q", part.Kind)
	}
	if string(part.ImageData) != string(pngHeader) {
		t.Errorf("data mismatch: %v vs %v", part.ImageData, pngHeader)
	}
	if part.ImageMIME != "image/png" {
		t.Errorf("mime: %q", part.ImageMIME)
	}
}

func TestExtractScreenshotImagePart_DataURI(t *testing.T) {
	uri := "data:image/png;base64,iVBORw0KG..."
	resp, _ := json.Marshal(map[string]any{"image": uri})
	part, ok := ExtractScreenshotImagePart(resp)
	if !ok {
		t.Fatal("expected ok")
	}
	if part.ImageURL != uri {
		t.Errorf("url: %q", part.ImageURL)
	}
}

func TestExtractScreenshotImagePart_EmptyImage(t *testing.T) {
	resp, _ := json.Marshal(map[string]any{"image": ""})
	_, ok := ExtractScreenshotImagePart(resp)
	if ok {
		t.Error("expected not ok for empty image")
	}
}

func TestExtractScreenshotImagePart_NoImageField(t *testing.T) {
	resp, _ := json.Marshal(map[string]any{"other": "x"})
	_, ok := ExtractScreenshotImagePart(resp)
	if ok {
		t.Error("expected not ok when image field missing")
	}
}

func TestExtractScreenshotImagePart_InvalidBase64(t *testing.T) {
	resp, _ := json.Marshal(map[string]any{"image": "!!!not-base64!!!"})
	_, ok := ExtractScreenshotImagePart(resp)
	if ok {
		t.Error("expected not ok for invalid base64")
	}
}

func TestExtractScreenshotImagePart_MalformedJSON(t *testing.T) {
	_, ok := ExtractScreenshotImagePart([]byte("not json"))
	if ok {
		t.Error("expected not ok for malformed JSON")
	}
}
