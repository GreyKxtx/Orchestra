package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const CacheVersion = 1

// Cache is a JSON snapshot of project file metadata (v0.3).
//
// Notes:
// - Files map uses relative paths with forward slashes ("/").
// - MTime is stored as Unix nanoseconds.
// - Hash is stored as "sha256:<hex>".
type Cache struct {
	Version     int                `json:"version"`
	ProjectRoot string             `json:"project_root"`
	ProjectID   string             `json:"project_id"`
	ConfigHash  string             `json:"config_hash"`
	IndexedAt   int64              `json:"indexed_at"` // unix seconds
	Files       map[string]FileRef `json:"files"`
}

type FileRef struct {
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // unix nanos
	Hash  string `json:"hash"`  // sha256:<hex>
}

type ConfigFingerprint struct {
	ExcludeDirs        []string `json:"exclude_dirs"`
	SkipBackups        bool     `json:"skip_backups"`
	MaxCacheFileBytes  int64    `json:"max_cache_file_bytes"`
	ProtocolVersion    int      `json:"protocol_version"`
	CacheVersion       int      `json:"cache_version"`
	PathNormalization  string   `json:"path_normalization"`
	MTimePrecision     string   `json:"mtime_precision"`
	HashFormat         string   `json:"hash_format"`
	ImplementationNote string   `json:"implementation_note"`
}

func NewCache(projectRoot, projectID, configHash string, files map[string]FileRef) *Cache {
	if files == nil {
		files = map[string]FileRef{}
	}
	return &Cache{
		Version:     CacheVersion,
		ProjectRoot: projectRoot,
		ProjectID:   projectID,
		ConfigHash:  configHash,
		IndexedAt:   time.Now().Unix(),
		Files:       files,
	}
}

func LoadCacheIfExists(path string) (*Cache, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read cache: %w", err)
	}

	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, true, fmt.Errorf("failed to parse cache JSON: %w", err)
	}
	if c.Version != CacheVersion {
		return nil, true, fmt.Errorf("unsupported cache version: %d", c.Version)
	}
	if c.Files == nil {
		c.Files = map[string]FileRef{}
	}
	return &c, true, nil
}

func SaveCache(path string, cache *Cache) error {
	if cache == nil {
		return fmt.Errorf("cache is nil")
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache JSON: %w", err)
	}
	data = append(data, '\n')

	if err := atomicWriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache atomically: %w", err)
	}
	return nil
}

func ComputeProjectID(projectRoot string) (string, error) {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute project root: %w", err)
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		// Windows paths are usually case-insensitive.
		abs = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(abs))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ComputeConfigHash(excludeDirs []string, skipBackups bool, maxCacheFileBytes int64, protocolVersion int) (string, error) {
	dirs := make([]string, 0, len(excludeDirs))
	dirs = append(dirs, excludeDirs...)
	sort.Strings(dirs)

	fp := ConfigFingerprint{
		ExcludeDirs:        dirs,
		SkipBackups:        skipBackups,
		MaxCacheFileBytes:  maxCacheFileBytes,
		ProtocolVersion:    protocolVersion,
		CacheVersion:       CacheVersion,
		PathNormalization:  "filepath.ToSlash(rel) for cache keys",
		MTimePrecision:     "unix_nanos",
		HashFormat:         "sha256:<hex>",
		ImplementationNote: "v0.3 snapshot (no SQLite)",
	}

	b, err := json.Marshal(fp)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config fingerprint: %w", err)
	}

	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ComputeSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, base+".tmp-*")
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

	// Best-effort atomic replace.
	if err := os.Rename(tmpName, path); err == nil {
		return nil
	}

	// Windows: os.Rename fails if destination exists.
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}
