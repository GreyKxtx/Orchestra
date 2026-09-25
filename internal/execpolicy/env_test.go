package execpolicy

import (
	"strings"
	"testing"
)

func TestScrubEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin", "HOME=/home/u", "GOFLAGS=-mod=mod", "NPM_TOKEN=npm",
		"OPENAI_API_KEY=sk-1", "ANTHROPIC_API_KEY=sk-2", "ORCH_API_KEY=k",
		"AWS_SECRET_ACCESS_KEY=a", "DB_PASSWORD=p", "MY_CLIENT_SECRET=c", "ORCH_MCP_TOKEN=t",
	}
	got := strings.Join(ScrubEnv(env, []string{"OPENAI_API_KEY"}), "\n")
	for _, keep := range []string{"PATH=", "HOME=", "GOFLAGS=", "NPM_TOKEN=", "OPENAI_API_KEY="} {
		if !strings.Contains(got, keep) {
			t.Errorf("%s must be kept:\n%s", keep, got)
		}
	}
	for _, drop := range []string{"ANTHROPIC_API_KEY", "ORCH_API_KEY", "AWS_SECRET_ACCESS_KEY", "DB_PASSWORD", "MY_CLIENT_SECRET", "ORCH_MCP_TOKEN"} {
		if strings.Contains(got, drop+"=") {
			t.Errorf("%s must be removed:\n%s", drop, got)
		}
	}
}
