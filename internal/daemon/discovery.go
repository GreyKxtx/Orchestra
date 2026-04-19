package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func discoveryPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".orchestra", "daemon.json")
}

func WriteDiscovery(projectRoot string, info DiscoveryInfo) error {
	if info.ProtocolVersion == 0 {
		info.ProtocolVersion = ProtocolVersion
	}
	if info.StartedAt == 0 {
		info.StartedAt = time.Now().Unix()
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal discovery JSON: %w", err)
	}
	data = append(data, '\n')

	path := discoveryPath(projectRoot)
	if err := atomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write discovery atomically: %w", err)
	}
	return nil
}

func ReadDiscovery(projectRoot string) (*DiscoveryInfo, bool, error) {
	path := discoveryPath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read discovery file: %w", err)
	}

	var info DiscoveryInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, true, fmt.Errorf("failed to parse discovery JSON: %w", err)
	}
	return &info, true, nil
}

func RemoveDiscovery(projectRoot string) error {
	path := discoveryPath(projectRoot)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to remove discovery file: %w", err)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	// Best-effort: lock down .orchestra/ directory on Unix.
	_ = os.Chmod(dir, 0700)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err == nil {
		_ = os.Chmod(path, perm)
		return nil
	}
	// Windows: os.Rename fails if destination exists.
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	_ = os.Chmod(path, perm)
	return nil
}

// AtomicWriteFile writes data to path atomically (best-effort across platforms).
//
// It is used for small local "discovery" files, where partial writes would break clients.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return atomicWriteFile(path, data, perm)
}
