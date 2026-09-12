package core

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newAttachmentsTestCore(t *testing.T) (*Core, string) {
	t.Helper()
	root := t.TempDir()
	body := "project_root: .\nllm:\n  api_base: http://127.0.0.1:1/v1\n  model: m\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestra.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, root
}

// A pasted screenshot from the browser: bytes in, an attachment under the
// workspace out, of the kind the turn will treat as an image.
func TestAttachmentsStore_WritesUnderTheWorkspace(t *testing.T) {
	c, root := newAttachmentsTestCore(t)
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}

	res, err := c.AttachmentsStore(AttachmentsStoreParams{
		Name:       "shot.png",
		MIME:       "image/png",
		DataBase64: base64.StdEncoding.EncodeToString(png),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Rel, ".orchestra/attachments/") || !strings.HasSuffix(res.Rel, "-shot.png") {
		t.Fatalf("rel = %q, want .orchestra/attachments/<stamp>-shot.png", res.Rel)
	}
	if !samePath(filepath.Dir(res.Path), filepath.Join(root, ".orchestra", "attachments")) {
		t.Fatalf("path = %q is not under the workspace's attachments folder", res.Path)
	}
	if res.Kind != "image" || res.Ext != "png" || res.Name != "shot.png" || res.Size != len(png) {
		t.Fatalf("result = %+v", res)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(png) {
		t.Fatal("stored bytes differ from what was sent")
	}
}

// Names come from the client and go into a path: only a base name, only
// safe characters, and an extension when there was none.
func TestAttachmentsStore_SpellsTheNameSafely(t *testing.T) {
	c, _ := newAttachmentsTestCore(t)
	data := base64.StdEncoding.EncodeToString([]byte("hello"))

	cases := []struct {
		name, mime, want, kind string
	}{
		{"../../etc/passwd", "", "passwd.bin", "file"},
		{"my notes?.txt", "text/plain", "my-notes_.txt", "file"},
		{"", "image/png", "attachment.png", "image"},
		{"C:\\Users\\me\\Pictures\\pic.JPG", "", "pic.JPG", "image"},
	}
	for _, tc := range cases {
		res, err := c.AttachmentsStore(AttachmentsStoreParams{Name: tc.name, MIME: tc.mime, DataBase64: data})
		if err != nil {
			t.Fatalf("%q: %v", tc.name, err)
		}
		if res.Name != tc.want {
			t.Errorf("%q → %q, want %q", tc.name, res.Name, tc.want)
		}
		if res.Kind != tc.kind {
			t.Errorf("%q: kind = %q, want %q", tc.name, res.Kind, tc.kind)
		}
	}
}

func TestAttachmentsStore_RefusesEmptyAndOversize(t *testing.T) {
	c, _ := newAttachmentsTestCore(t)
	if _, err := c.AttachmentsStore(AttachmentsStoreParams{Name: "x.txt", DataBase64: ""}); err == nil {
		t.Fatal("an empty attachment must be refused")
	}
	if _, err := c.AttachmentsStore(AttachmentsStoreParams{Name: "x.txt", DataBase64: "not base64!!"}); err == nil {
		t.Fatal("bad base64 must be refused")
	}
	big := make([]byte, maxStoredAttachmentBytes+1)
	if _, err := c.AttachmentsStore(AttachmentsStoreParams{Name: "big.bin", DataBase64: base64.StdEncoding.EncodeToString(big)}); err == nil {
		t.Fatal("an oversize attachment must be refused")
	}
}
