package format

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestExtractMCPImageParts(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("PNGBYTES"))
	out := json.RawMessage(`{"result":"ok","images":[{"data":"` + png + `","mime":"image/png"}]}`)

	parts := ExtractMCPImageParts(out)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	if parts[0].Kind != llm.PartImage || parts[0].ImageMIME != "image/png" {
		t.Errorf("part = %+v", parts[0])
	}
	if string(parts[0].ImageData) != "PNGBYTES" {
		t.Errorf("data = %q", parts[0].ImageData)
	}
}

func TestExtractMCPImageParts_NothingToExtract(t *testing.T) {
	cases := map[string]string{
		"plain text result": `{"result":"ok"}`,
		"empty images":      `{"result":"ok","images":[]}`,
		"undecodable":       `{"result":"ok","images":[{"data":"!!!","mime":"image/png"}]}`,
		"not json":          `not json at all`,
		"other tool":        `{"path":"a.go","bytes":10}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if parts := ExtractMCPImageParts(json.RawMessage(raw)); len(parts) != 0 {
				t.Errorf("parts = %+v, want none", parts)
			}
		})
	}
}

func TestExtractMCPImageParts_CapsHowManyReachTheModel(t *testing.T) {
	// A server that returns a gallery would otherwise blow the context window
	// in one tool call.
	png := base64.StdEncoding.EncodeToString([]byte("PNGBYTES"))
	raw := `{"result":"ok","images":[`
	for i := 0; i < 12; i++ {
		if i > 0 {
			raw += ","
		}
		raw += `{"data":"` + png + `","mime":"image/png"}`
	}
	raw += `]}`

	parts := ExtractMCPImageParts(json.RawMessage(raw))
	if len(parts) != maxMCPImagesPerCall {
		t.Fatalf("parts = %d, want the cap of %d", len(parts), maxMCPImagesPerCall)
	}
}
