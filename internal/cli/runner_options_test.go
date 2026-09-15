package cli

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// The browser client on the Runner is the only thing that makes browser.* work:
// the agent offers those tools on --allow-browser, and a Runner built without
// the client answers every call with "browser tools require --allow-browser".
// `orchestra workflow --allow-browser` built its Runner by hand and left the
// flag out, so its stages were offered ten tools that could only fail.
func TestCLIRunnerOptions_CarryTheBrowserFlagAndTheProjectsSettings(t *testing.T) {
	cfg := &config.ProjectConfig{}
	cfg.Browser = config.BrowserConfig{Headless: true, AllowEval: true}
	cfg.Exec.TimeoutS = 7

	on := cliRunnerOptions(cfg, true, true)
	if !on.AllowBrowser || !on.Browser.AllowEval || !on.DryRun {
		t.Errorf("flags lost: allow_browser=%v allow_eval=%v dry_run=%v", on.AllowBrowser, on.Browser.AllowEval, on.DryRun)
	}
	if on.ExecTimeout.Seconds() != 7 {
		t.Errorf("exec timeout %v, want 7s from the config", on.ExecTimeout)
	}
	if off := cliRunnerOptions(cfg, false, false); off.AllowBrowser || off.DryRun {
		t.Errorf("a run without --allow-browser got a browser, or an applying run stayed dry: %+v", off)
	}
}
