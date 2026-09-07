package cli

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/orchestra/orchestra/protocol"
	"github.com/spf13/cobra"
)

// buildVersion is stamped by release builds:
//
//	go build -ldflags "-X github.com/orchestra/orchestra/internal/cli.buildVersion=v0.3.0"
//
// A plain `go build` leaves it empty and falls back to protocol.CoreVersion.
var buildVersion = ""

// versionString reports the build alongside the three numbers `initialize`
// compares on connect. A client that refuses to attach fails on one of them,
// so they belong in the first thing a user is asked to paste into a report.
// currentVersion is the build's own version token, without the commit hash and
// protocol numbers versionString wraps around it — the one piece that can be
// compared against a release tag.
func currentVersion() string {
	if v := strings.TrimSpace(buildVersion); v != "" {
		return v
	}
	return protocol.CoreVersion
}

func versionString() string {
	v := currentVersion()
	if rev := vcsRevision(); rev != "" {
		v += " (" + rev + ")"
	}
	return fmt.Sprintf(
		"orchestra %s\nprotocol %d · ops %d · tools %d\n%s/%s %s",
		v,
		protocol.ProtocolVersion, protocol.OpsVersion, protocol.ToolsVersion,
		runtime.GOOS, runtime.GOARCH, runtime.Version(),
	)
}

// vcsRevision returns the short commit Go stamps into the binary, plus a
// +dirty marker when the tree had uncommitted changes at build time.
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
			if len(rev) > 7 {
				rev = rev[:7]
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "+dirty"
			}
		}
	}
	return rev + dirty
}

var versionCheck bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the build and protocol versions",
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, versionString())
		if !versionCheck {
			return nil
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		return runVersionCheck(ctx, out)
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionCheck, "check", false,
		"сравнить с последним релизом на GitHub (требует сети)")
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
