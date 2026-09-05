package mcp

import (
	"context"
	"strings"
	"testing"
)

// resourceStub is a ServerClient that also serves resources.
type resourceStub struct {
	richStub
	name string
	res  []MCPResource
	body string
}

func (r *resourceStub) ServerName() string { return r.name }
func (r *resourceStub) ListResources(ctx context.Context) ([]MCPResource, error) {
	return r.res, nil
}
func (r *resourceStub) ReadResource(ctx context.Context, uri string) (string, error) {
	if uri != "file:///notes.md" {
		return "", errNoSuchResource(uri)
	}
	return r.body, nil
}

func errNoSuchResource(uri string) error { return &noResourceError{uri} }

type noResourceError struct{ uri string }

func (e *noResourceError) Error() string { return "no such resource: " + e.uri }

func newResourceManager() *Manager {
	return &Manager{clients: []ServerClient{&resourceStub{
		name: "docs",
		res: []MCPResource{
			{URI: "file:///notes.md", Name: "Notes", MIMEType: "text/markdown"},
			{URI: "file:///spec.md", Name: "Spec"},
		},
		body: "# Notes\nthe body",
	}}}
}

func TestManagerListResources_NamesTheServer(t *testing.T) {
	got := newResourceManager().ListResources(context.Background())
	if len(got) != 2 {
		t.Fatalf("resources = %d, want 2", len(got))
	}
	if got[0].Server != "docs" {
		t.Errorf("server = %q, want docs", got[0].Server)
	}
	if got[0].URI != "file:///notes.md" || got[0].Name != "Notes" {
		t.Errorf("resource = %+v", got[0])
	}
}

func TestManagerReadResource(t *testing.T) {
	m := newResourceManager()
	body, err := m.ReadResource(context.Background(), "docs", "file:///notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "the body") {
		t.Errorf("body = %q", body)
	}
}

func TestManagerReadResource_UnknownServerIsNamedInTheError(t *testing.T) {
	m := newResourceManager()
	_, err := m.ReadResource(context.Background(), "nope", "file:///x")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want it to name the server", err)
	}
}

func TestManagerResources_ServerWithoutResourceSupportIsNotAnError(t *testing.T) {
	// resources/* is optional in MCP. A server that does not implement it
	// must not break listing for the servers that do.
	m := &Manager{clients: []ServerClient{&richStub{}}}
	if got := m.ListResources(context.Background()); len(got) != 0 {
		t.Errorf("resources = %+v, want none", got)
	}
	if _, err := m.ReadResource(context.Background(), "stub", "file:///x"); err == nil {
		t.Error("reading from a server without resource support must report why")
	}
}
