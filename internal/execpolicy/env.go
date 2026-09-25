package execpolicy

import (
	"runtime"
	"strings"
)

// A command the model runs used to inherit the core's whole environment,
// provider keys included: one `env` or `printenv` put them in the transcript,
// and from there in the provider's logs. ScrubEnv drops variables that name a
// secret; exec.env_passthrough brings back the ones a project's own commands
// need (a test suite that calls an API, say).

var secretSuffixes = []string{
	"_API_KEY", "_APIKEY", "_SECRET", "_SECRET_KEY", "_ACCESS_KEY", "_PRIVATE_KEY",
	"_PASSWORD", "_PASSWD", "_CLIENT_SECRET",
}

var secretNames = map[string]bool{
	"ORCH_API_KEY": true, "ORCH_MCP_TOKEN": true, "ANTHROPIC_AUTH_TOKEN": true,
	"AWS_SESSION_TOKEN": true, "OPENAI_ACCESS_TOKEN": true, "HF_TOKEN": true,
	"HUGGING_FACE_HUB_TOKEN": true,
}

// IsSecretEnvName reports whether a variable name looks like it holds a
// credential.
func IsSecretEnvName(name string) bool {
	u := strings.ToUpper(strings.TrimSpace(name))
	if u == "" {
		return false
	}
	if secretNames[u] || strings.Contains(u, "SECRET") {
		return true
	}
	for _, s := range secretSuffixes {
		if strings.HasSuffix(u, s) {
			return true
		}
	}
	return false
}

// ScrubEnv returns env ("NAME=value" entries) without secret-looking
// variables, except those named in keep.
func ScrubEnv(env []string, keep []string) []string {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[envKey(k)] = true
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name != "" && IsSecretEnvName(name) && !kept[envKey(name)] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// envKey compares names the way the platform does: case-insensitively on
// Windows.
func envKey(name string) string {
	name = strings.TrimSpace(name)
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}
