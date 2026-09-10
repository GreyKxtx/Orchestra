package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/trajectory"
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

// TestSessionClose_DoesNotReportSuccessWhenTheSnapshotSurvives pins Fix B:
// SessionClose must not report success when the on-disk snapshot could not
// actually be removed.
//
// Windows-only: POSIX unlinks open files, so the removal below would succeed
// there and there would be nothing to assert. sessionfile.Delete removes the
// trajectory sidecar first and only then the snapshot; holding the sidecar
// open makes that first os.Remove fail on Windows, which is exactly the
// situation a live turn creates (the trajectory writer holds the sidecar
// open for the whole turn, and Session.Cancel does not wait for it to
// unwind). CI runs both platforms.
func TestSessionClose_DoesNotReportSuccessWhenTheSnapshotSurvives(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("POSIX allows removing an open file, so there is nothing to assert here")
	}

	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, nil)

	sess := c.sessions.Create()
	sess.Lock()
	err := sess.Snapshot(root)
	sess.Unlock()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	snapshotPath := filepath.Join(root, ".orchestra", "sessions", sess.ID+".json")
	if _, statErr := os.Stat(snapshotPath); statErr != nil {
		t.Fatalf("expected snapshot on disk before close, stat: %v", statErr)
	}

	// Hold the sidecar open the way a live turn's trajectory writer would.
	w, err := trajectory.NewWriter(root, sess.ID)
	if err != nil {
		t.Fatalf("trajectory.NewWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	closeErr := c.SessionClose(SessionCloseParams{SessionID: sess.ID})
	if closeErr == nil {
		t.Fatal("expected SessionClose to report an error while the sidecar is held open, got nil")
	}
	if _, statErr := os.Stat(snapshotPath); statErr != nil {
		t.Fatalf("snapshot should survive a failed close, stat: %v", statErr)
	}
}
