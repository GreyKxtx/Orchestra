package sessionfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
)

func sessionsDir(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "sessions")
}

func snapshotPath(workspaceRoot, id string) string {
	return filepath.Join(sessionsDir(workspaceRoot), id+".json")
}

// Save writes snap atomically to .orchestra/sessions/<id>.json.
func Save(workspaceRoot string, snap *Snapshot) error {
	if snap == nil {
		return fmt.Errorf("sessionfile: nil snapshot")
	}
	if strings.TrimSpace(snap.ID) == "" {
		return fmt.Errorf("sessionfile: empty id")
	}
	normalizeSnapshot(snap, snap.ID)
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now().UTC()
	}
	snap.UpdatedAt = time.Now().UTC()
	snap.MsgCount = len(snap.UIMessages)
	snap.Version = Version

	dir := sessionsDir(workspaceRoot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("sessionfile: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("sessionfile: marshal: %w", err)
	}
	return fsutil.AtomicWriteFile(snapshotPath(workspaceRoot, snap.ID), data, 0o600)
}

// Load reads and migrates a session snapshot by id.
func Load(workspaceRoot, id string) (*Snapshot, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("sessionfile: empty id")
	}
	data, err := os.ReadFile(snapshotPath(workspaceRoot, id))
	if err != nil {
		return nil, err
	}
	return ParseSnapshot(data, id)
}

// Delete removes a session file. Missing file is not an error.
func Delete(workspaceRoot, id string) error {
	if workspaceRoot == "" || id == "" {
		return nil
	}
	if err := os.Remove(snapshotPath(workspaceRoot, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListMeta returns metadata for every session file, newest first.
func ListMeta(workspaceRoot string) ([]Meta, error) {
	dir := sessionsDir(workspaceRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		snap, err := ParseSnapshot(data, id)
		if err != nil {
			continue
		}
		out = append(out, Meta{
			ID:        snap.ID,
			Title:     snap.Title,
			Model:     snap.Model,
			CreatedAt: snap.CreatedAt,
			UpdatedAt: snap.UpdatedAt,
			MsgCount:  snap.MsgCount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}
