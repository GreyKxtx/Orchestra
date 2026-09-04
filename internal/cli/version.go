package cli

import (
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
func versionString() string {
	v := strings.TrimSpace(buildVersion)
	if v == "" {
		v = protocol.CoreVersion
	}
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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the build and protocol versions",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), versionString())
	},
}

func init() {
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
