package attachments

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/fsutil"
)

// StageIntoWorkspace ensures srcPath is readable inside workspaceRoot.
// External files are copied to .orchestra/attachments/<timestamp>-<name>.
func StageIntoWorkspace(workspaceRoot, srcPath string) (string, error) {
	srcPath = strings.TrimSpace(srcPath)
	if srcPath == "" {
		return "", fmt.Errorf("attachment path is empty")
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return "", fmt.Errorf("workspace root is empty")
	}

	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return "", fmt.Errorf("attachment path: %w", err)
	}
	info, err := os.Stat(absSrc)
	if err != nil {
		return "", fmt.Errorf("attachment %q: %w", absSrc, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("attachment %q: is a directory", absSrc)
	}
	if info.Size() > MaxImageBytes {
		return "", fmt.Errorf("attachment %q: exceeds %d MB limit", absSrc, MaxImageBytes/(1024*1024))
	}

	if resolved, _, err := fsutil.ResolveInWorkspace(root, absSrc); err == nil {
		return resolved, nil
	}

	dir := filepath.Join(root, ".orchestra", "attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("attachments dir: %w", err)
	}
	base := filepath.Base(absSrc)
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_', r == '+', r == '(', r == ')', r == ' ':
			return r
		default:
			return '_'
		}
	}, base)
	safe = strings.ReplaceAll(strings.TrimSpace(safe), " ", "-")
	if safe == "" {
		safe = "attachment"
	}
	dest := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixMilli(), safe))
	if err := copyFile(absSrc, dest); err != nil {
		return "", err
	}
	resolved, _, err := fsutil.ResolveInWorkspace(root, dest)
	if err != nil {
		return "", fmt.Errorf("staged attachment outside workspace: %w", err)
	}
	return resolved, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open attachment: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create staged attachment: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("copy attachment: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("sync staged attachment: %w", err)
	}
	return nil
}

// MessageAttachmentFromPath builds RPC metadata for a staged workspace path.
func MessageAttachmentFromPath(wsPath string) MessageAttachment {
	wsPath = strings.TrimSpace(wsPath)
	name := filepath.Base(wsPath)
	ext := strings.ToLower(filepath.Ext(name))
	att := MessageAttachment{
		Path: wsPath,
		Name: name,
		MIME: MIMEForPath(wsPath),
	}
	att.Kind = ResolveKind(att)
	if ext != "" {
		// Kind is resolved; ext is for UI only — stored separately in session UI.
		_ = ext
	}
	return att
}
