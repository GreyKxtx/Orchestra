package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// `orchestra auth set-key` wrote the secret straight into .orchestra.yml —
// the committed, shared config — so the documented way to configure a
// provider published the key with the next commit. The key now goes into the
// gitignored .orchestra.env and the config keeps a ${NAME} reference.

const configWithProvider = `project_root: .
llm:
  api_base: http://x/v1
  model: m
providers:
  gemini:
    api_base: https://example.invalid/v1
    model: gemini-test
`

func setKey(t *testing.T, provider, key string) {
	t.Helper()
	authSetKeyValue = key
	t.Cleanup(func() { authSetKeyValue = "" })
	if err := runAuthSetKey(nil, []string{provider}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthSetKey_KeepsTheSecretOutOfTheCommittedConfig(t *testing.T) {
	isolateHome(t)
	dir := chdirWithConfig(t, configWithProvider)

	setKey(t, "gemini", "sk-live-secret")

	cfgBody := readTestFile(t, filepath.Join(dir, ".orchestra.yml"))
	if strings.Contains(cfgBody, "sk-live-secret") {
		t.Errorf("the key was written into the committed config:\n%s", cfgBody)
	}
	if !strings.Contains(cfgBody, "${GEMINI_API_KEY}") {
		t.Errorf("the config does not refer to the variable:\n%s", cfgBody)
	}

	envBody := readTestFile(t, filepath.Join(dir, config.EnvFileName))
	if !strings.Contains(envBody, "GEMINI_API_KEY=sk-live-secret") {
		t.Errorf("the key is not in %s:\n%s", config.EnvFileName, envBody)
	}
}

// The round trip is what makes it usable: after set-key, a plain Load must
// hand the agent the real key.
func TestAuthSetKey_TheStoredKeyLoadsBack(t *testing.T) {
	isolateHome(t)
	dir := chdirWithConfig(t, configWithProvider)

	setKey(t, "gemini", "sk-live-secret")

	cfg, err := config.Load(filepath.Join(dir, ".orchestra.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers["gemini"].APIKey; got != "sk-live-secret" {
		t.Errorf("api_key after reload = %q, want the stored key", got)
	}
}

// Setting the same key twice must not leave two variables for one field.
func TestAuthSetKey_ReusesTheVariableTheConfigNames(t *testing.T) {
	isolateHome(t)
	dir := chdirWithConfig(t, `project_root: .
llm:
  api_base: http://x/v1
  model: m
providers:
  gemini:
    api_base: https://example.invalid/v1
    api_key: ${MY_OWN_NAME}
    model: gemini-test
`)

	setKey(t, "gemini", "sk-second")

	envBody := readTestFile(t, filepath.Join(dir, config.EnvFileName))
	if !strings.Contains(envBody, "MY_OWN_NAME=sk-second") {
		t.Errorf("the key did not go into the variable the config names:\n%s", envBody)
	}
	if strings.Contains(envBody, "GEMINI_API_KEY") {
		t.Errorf("a second variable was invented for the same field:\n%s", envBody)
	}
}

// The file is the user's: other keys and their comments survive an update.
func TestAuthSetKey_LeavesTheRestOfTheEnvFileAlone(t *testing.T) {
	isolateHome(t)
	dir := chdirWithConfig(t, configWithProvider)
	envPath := filepath.Join(dir, config.EnvFileName)
	if err := os.WriteFile(envPath, []byte("# my keys\nOPENROUTER_API_KEY=sk-or\nGEMINI_API_KEY=sk-old\n"), 0600); err != nil {
		t.Fatal(err)
	}

	setKey(t, "gemini", "sk-new")

	envBody := readTestFile(t, envPath)
	for _, want := range []string{"# my keys", "OPENROUTER_API_KEY=sk-or", "GEMINI_API_KEY=sk-new"} {
		if !strings.Contains(envBody, want) {
			t.Errorf("missing %q after the update:\n%s", want, envBody)
		}
	}
	if strings.Contains(envBody, "sk-old") {
		t.Errorf("the previous value was kept alongside the new one:\n%s", envBody)
	}
}

func TestAuthSetKey_MainLLMGetsItsOwnVariable(t *testing.T) {
	isolateHome(t)
	dir := chdirWithConfig(t, plainConfig)

	setKey(t, "llm", "sk-main")

	if body := readTestFile(t, filepath.Join(dir, ".orchestra.yml")); strings.Contains(body, "sk-main") {
		t.Errorf("the main key was written into the config:\n%s", body)
	}
	if body := readTestFile(t, filepath.Join(dir, config.EnvFileName)); !strings.Contains(body, "ORCHESTRA_API_KEY=sk-main") {
		t.Errorf("the main key is not in %s:\n%s", config.EnvFileName, body)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
