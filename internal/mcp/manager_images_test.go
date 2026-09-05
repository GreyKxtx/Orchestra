package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// richStub implements ServerClient plus CallRich.
type richStub struct {
	text   string
	images []MCPImage
}

func (r *richStub) ServerName() string                { return "stub" }
func (r *richStub) Tools() []MCPTool                  { return nil }
func (r *richStub) AllToolNames() []string            { return nil }
func (r *richStub) SetAllowedTools(names []string)    {}
func (r *richStub) IsDead() bool                      { return false }
func (r *richStub) StderrTail() string                { return "" }
func (r *richStub) Close() error                      { return nil }
func (r *richStub) Call(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	return r.text, nil
}
func (r *richStub) CallRich(ctx context.Context, tool string, args json.RawMessage) (string, []MCPImage, error) {
	return r.text, r.images, nil
}

func managerWith(c ServerClient) *Manager {
	return &Manager{clients: []ServerClient{c}}
}

func TestManagerCall_CarriesImagesAlongsideTheText(t *testing.T) {
	m := managerWith(&richStub{
		text:   "page rendered",
		images: []MCPImage{{Data: []byte("PNGBYTES"), MIME: "image/png"}},
	})
	out, err := m.Call(context.Background(), "mcp:stub:screenshot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result string `json:"result"`
		Images []struct {
			Data string `json:"data"`
			MIME string `json:"mime"`
		} `json:"images"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Result != "page rendered" {
		t.Errorf("result = %q", got.Result)
	}
	if len(got.Images) != 1 || got.Images[0].MIME != "image/png" {
		t.Fatalf("images = %+v", got.Images)
	}
	if got.Images[0].Data == "" {
		t.Error("image data is empty")
	}
}

func TestManagerCall_TextOnlyResultKeepsItsOldShape(t *testing.T) {
	// The result JSON is what the model reads. A tool that returns no image
	// must not start carrying an empty images key.
	m := managerWith(&richStub{text: "just text"})
	out, err := m.Call(context.Background(), "mcp:stub:read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "images") {
		t.Errorf("out = %s, want no images key", out)
	}
}
