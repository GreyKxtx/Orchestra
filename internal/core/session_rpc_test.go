package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/protocol"
)

func TestInitialize_ToolsVersion14Handshake(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatalf("New core: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	h := NewRPCHandler(c)

	projectID, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	if protocol.ToolsVersion != 14 {
		t.Fatalf("protocol.ToolsVersion = %d, want 14", protocol.ToolsVersion)
	}

	params, err := json.Marshal(InitializeParams{
		ProjectRoot:     root,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    14,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := h.Handle(context.Background(), "initialize", params); err != nil {
		t.Fatalf("handshake with tools_version=14 failed: %v", err)
	}
}

func TestInitialize_ToolsVersion13Mismatch(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatalf("New core: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	h := NewRPCHandler(c)

	projectID, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	params, err := json.Marshal(InitializeParams{
		ProjectRoot:     root,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    13,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = h.Handle(context.Background(), "initialize", params)
	if err == nil {
		t.Fatal("expected ProtocolMismatch for tools_version=13")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.ProtocolMismatch {
		t.Fatalf("want ProtocolMismatch, got %v", err)
	}
}
