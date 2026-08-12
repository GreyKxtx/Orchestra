package contract

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/tools/exec"
)

const spectralTimeout = 90 * time.Second

// SpectralLint runs `spectral lint` over the OpenAPI artifact when the
// toolchain is available (spec §5.4: openapi: spectral). Returns:
//   - ""            — lint green, or toolchain unavailable (built-in checks
//     from VerifyArtifacts remain the fail-closed floor);
//   - non-empty     — lint findings that must block the freeze.
func SpectralLint(ctx context.Context, projectRoot string) string {
	target := filepath.ToSlash(filepath.Join(DirRel, ArtifactOpenAPI))
	resp, err := exec.Run(ctx, projectRoot, spectralTimeout, 32*1024, exec.RunRequest{
		Command:   "npx",
		Args:      []string{"--no-install", "@stoplight/spectral-cli", "lint", "--fail-severity", "error", target},
		TimeoutMS: int(spectralTimeout / time.Millisecond),
	})
	if err != nil || resp == nil {
		// Toolchain unavailable (no node / package not installed) — skip,
		// do not block: built-ins already validated the document shape.
		return ""
	}
	if resp.ExitCode == 0 {
		return ""
	}
	out := strings.TrimSpace(resp.Stdout)
	if out == "" {
		out = strings.TrimSpace(resp.Stderr)
	}
	// Exit codes 1/2 are lint findings; anything else with empty output is
	// an environment failure — treat as unavailable rather than red.
	if out == "" {
		return ""
	}
	if len(out) > 1200 {
		out = out[:1200] + "..."
	}
	return out
}
