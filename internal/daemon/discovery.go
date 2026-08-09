package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
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

// AtomicWriteFile delegates to fsutil.AtomicWriteFile. Kept as a
// re-export so existing callers don't need to change yet; the
// implementation lives in internal/fsutil now (H4 in architecture
// audit). New callers should import fsutil directly.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return fsutil.AtomicWriteFile(path, data, perm)
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return fsutil.AtomicWriteFile(path, data, perm)
}
