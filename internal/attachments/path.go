package attachments

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/fsutil"
)

// ValidatePaths ensures every attachment path resolves inside workspaceRoot.
func ValidatePaths(workspaceRoot string, atts []MessageAttachment) error {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return fmt.Errorf("workspace root is empty")
	}
	for _, a := range atts {
		p := strings.TrimSpace(a.Path)
		if p == "" {
			return fmt.Errorf("attachment path is empty")
		}
		if _, _, err := fsutil.ResolveInWorkspace(root, p); err != nil {
			return fmt.Errorf("attachment %q: %w", p, err)
		}
	}
	return nil
}
