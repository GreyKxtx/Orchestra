//go:build !windows

package exec

import "os/exec"

// keepShellLine is Windows' business: sh -c takes its line as one argument.
func keepShellLine(_ *exec.Cmd, _ bool) {}
