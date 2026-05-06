package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func listFiles(workspaceRoot string, startAbs string, excludeDirs []string, skipBackups bool, includeHash bool, recursive bool, limit int) ([]FSFileMeta, error) {
	excludeMap := make(map[string]bool, len(excludeDirs))
	for _, d := range excludeDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		excludeMap[strings.Trim(d, "/\\")] = true
	}

	var out []FSFileMeta
	workspaceRoot = filepath.Clean(workspaceRoot)
	startAbs = filepath.Clean(startAbs)

	if !recursive {
		entries, err := os.ReadDir(startAbs)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				dirName := e.Name()
				if excludeMap[dirName] {
					continue
				}
				continue
			}
			p := filepath.Join(startAbs, e.Name())
			if skipBackups && strings.HasSuffix(p, ".orchestra.bak") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(workspaceRoot, p)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			meta := FSFileMeta{
				Path:  rel,
				Size:  info.Size(),
				MTime: info.ModTime().Unix(), // Use seconds, not nanoseconds, for better JSON compatibility
			}
			if includeHash {
				h, err := sha256File(p)
				if err == nil {
					meta.FileHash = h
				}
			}
			out = append(out, meta)
		}
	} else {
		err := filepath.WalkDir(startAbs, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // best-effort
			}
			if d.IsDir() {
				rel, _ := filepath.Rel(workspaceRoot, p)
				if rel == "." {
					return nil
				}
				dirName := filepath.Base(p)
				relSlash := filepath.ToSlash(rel)
				if excludeMap[dirName] || excludeMap[rel] || excludeMap[relSlash] {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip backups
			if skipBackups && strings.HasSuffix(p, ".orchestra.bak") {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}

			rel, err := filepath.Rel(workspaceRoot, p)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)

			meta := FSFileMeta{
				Path:  rel,
				Size:  info.Size(),
				MTime: info.ModTime().Unix(), // Use seconds, not nanoseconds, for better JSON compatibility
			}
			if includeHash {
				h, err := sha256File(p)
				if err == nil {
					meta.FileHash = h
				}
			}
			out = append(out, meta)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func readFileWithHash(absPath string, maxBytes int64) (content string, size int64, mtimeUnix int64, hash string, truncated bool, _ error) {
	st, err := os.Stat(absPath)
	if err != nil {
		return "", 0, 0, "", false, err
	}
	if st.IsDir() {
		return "", 0, 0, "", false, fmt.Errorf("path is a directory")
	}
	size = st.Size()
	mtimeUnix = st.ModTime().Unix()
	if maxBytes < 0 {
		maxBytes = 0
	}
	if maxBytes > 0 && size > maxBytes {
		truncated = true
	}

	f, err := os.Open(absPath)
	if err != nil {
		return "", 0, 0, "", false, err
	}
	defer f.Close()

	h := sha256.New()

	var b strings.Builder
	var kept int64

	buf := make([]byte, 32*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])

			if maxBytes <= 0 || kept < maxBytes {
				remain := maxBytes - kept
				if maxBytes <= 0 {
					remain = int64(n)
				}
				take := int64(n)
				if maxBytes > 0 && take > remain {
					take = remain
				}
				if take > 0 {
					b.Write(buf[:take])
					kept += take
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return "", 0, 0, "", false, rerr
		}
	}

	hash = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return b.String(), size, mtimeUnix, hash, truncated, nil
}

// addLineNumbers prefixes each line with its 1-based line number.
// Width is padded to the digit count of the last line number so columns align.
// A trailing newline in content is preserved; the empty "line" after it is not numbered.
func addLineNumbers(content string) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	hasTrailing := strings.HasSuffix(content, "\n")

	// Lines to number: exclude the empty string produced by a trailing \n.
	numerable := lines
	if hasTrailing {
		numerable = lines[:len(lines)-1]
	}
	if len(numerable) == 0 {
		return content
	}

	width := len(fmt.Sprintf("%d", len(numerable)))
	format := fmt.Sprintf("%%%dd: ", width)

	var b strings.Builder
	b.Grow(len(content) + len(numerable)*(width+2))
	for i, line := range numerable {
		fmt.Fprintf(&b, format, i+1)
		b.WriteString(line)
		b.WriteByte('\n')
	}

	result := b.String()
	if !hasTrailing {
		result = result[:len(result)-1] // strip the extra \n we added for the last line
	}
	return result
}
