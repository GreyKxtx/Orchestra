package provision

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp/registry"
	"github.com/orchestra/orchestra/internal/permission"
)

// MissingReports which detected/configured entries still need Ensure.
func Missing(entries []registry.Entry) []registry.Entry {
	var out []registry.Entry
	for _, e := range entries {
		cmd := append([]string{e.BinaryName}, e.DefaultArgs...)
		if _, err := Resolve(cmd); err != nil {
			out = append(out, e)
		}
	}
	return out
}

// EnsurePolicy is ask | true | false.
type EnsurePolicy string

// EnsureDetected installs language servers for workspace languages.
// policy: ask → one consent for the whole batch; true → silent; false → no-op.
// Only IDs with an installer succeed; others are skipped with a stderr-style error return list.
func EnsureDetected(ctx context.Context, workspaceRoot string, policy EnsurePolicy, consent permission.Requester) (installed []string, skipped []string, err error) {
	detected := Detect(workspaceRoot)
	missing := Missing(detected)
	if len(missing) == 0 {
		return nil, nil, nil
	}

	switch policy {
	case "false", "no", "0", "never", "off":
		for _, e := range missing {
			skipped = append(skipped, e.ID)
		}
		return nil, skipped, nil
	case "true", "yes", "1", "always":
		// proceed
	default: // ask
		if consent == nil {
			for _, e := range missing {
				skipped = append(skipped, e.ID+"(no-consent)")
			}
			return nil, skipped, nil
		}
		names := make([]string, len(missing))
		for i, e := range missing {
			names[i] = e.ID
		}
		desc := fmt.Sprintf("Установить language servers для проекта?\n%s\n→ ~/.orchestra/lsp/", strings.Join(names, ", "))
		resp, perr := consent.RequestPermission(ctx, permission.Request{
			Tool:        "lsp.install",
			Kind:        "lsp.install",
			Description: desc,
			Reason:      strings.Join(names, ","),
		})
		if perr != nil {
			return nil, nil, perr
		}
		if !resp.Approved {
			for _, e := range missing {
				skipped = append(skipped, e.ID)
			}
			return nil, skipped, nil
		}
	}

	for _, e := range missing {
		if !CanEnsure(e.ID) {
			skipped = append(skipped, e.ID+"(manual:"+e.InstallHint+")")
			continue
		}
		if err := Ensure(ctx, e.ID); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s(%v)", e.ID, err))
			continue
		}
		installed = append(installed, e.ID)
	}
	return installed, skipped, nil
}
