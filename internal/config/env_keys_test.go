package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A key written into .orchestra.yml is a key in the repository: that file is
// committed, and `orchestra auth set-key` put the secret straight into it.
// Nothing in the config could name an environment variable instead, so the
// only safe place left was the gitignored overlay.
//
// These tests pin the other arrangement: the file names the variable, the
// process holds the secret, and no Save ever turns the reference back into
// the secret it resolved to.

func TestLoad_APIKeyReadsTheNamedEnvironmentVariable(t *testing.T) {
	t.Setenv("ORCH_TEST_LLM_KEY", "sk-the-real-secret")
	path := writeMinimalConfig(t, minimalConfig+"  api_key: ${ORCH_TEST_LLM_KEY}\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKey != "sk-the-real-secret" {
		t.Errorf("api_key = %q, want the value of ORCH_TEST_LLM_KEY", cfg.LLM.APIKey)
	}
}

// An unset variable must not leave "${NAME}" standing: that string would be
// sent as the bearer token and come back as an opaque 401.
func TestLoad_AnUnsetVariableLeavesTheKeyEmpty(t *testing.T) {
	path := writeMinimalConfig(t, minimalConfig+"  api_key: ${ORCH_TEST_KEY_NOBODY_SET}\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKey != "" {
		t.Errorf("api_key = %q, want empty for an unset variable", cfg.LLM.APIKey)
	}
}

func TestLoad_ProviderKeysReadTheirVariablesToo(t *testing.T) {
	t.Setenv("ORCH_TEST_OPENROUTER_KEY", "sk-or-secret")
	path := writeMinimalConfig(t, minimalConfig+`providers:
  openrouter:
    api_base: https://openrouter.ai/api/v1
    api_key: ${ORCH_TEST_OPENROUTER_KEY}
    model: some/model
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers["openrouter"].APIKey; got != "sk-or-secret" {
		t.Errorf("providers.openrouter.api_key = %q, want the value of ORCH_TEST_OPENROUTER_KEY", got)
	}
}

// The TUI saves a setting by loading the config, changing one field and
// calling Save. Save writes what the in-memory config holds — so without this
// the first Shift+Tab after a load would replace the reference with the
// resolved secret and commit it on the next `git add`.
func TestSave_WritesTheReferenceBackNotTheSecret(t *testing.T) {
	t.Setenv("ORCH_TEST_LLM_KEY", "sk-the-real-secret")
	path := writeMinimalConfig(t, minimalConfig+"  api_key: ${ORCH_TEST_LLM_KEY}\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Agent.MaxSteps = 7 // any unrelated change the UI might make
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	body := readFile(t, path)
	if strings.Contains(body, "sk-the-real-secret") {
		t.Errorf("Save wrote the resolved secret into the config:\n%s", body)
	}
	if !strings.Contains(body, "${ORCH_TEST_LLM_KEY}") {
		t.Errorf("Save lost the ${ORCH_TEST_LLM_KEY} reference:\n%s", body)
	}
}

func TestSave_KeepsProviderReferencesToo(t *testing.T) {
	t.Setenv("ORCH_TEST_OPENROUTER_KEY", "sk-or-secret")
	path := writeMinimalConfig(t, minimalConfig+`providers:
  openrouter:
    api_base: https://openrouter.ai/api/v1
    api_key: ${ORCH_TEST_OPENROUTER_KEY}
    model: some/model
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Agent.MaxSteps = 7
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	body := readFile(t, path)
	if strings.Contains(body, "sk-or-secret") {
		t.Errorf("Save wrote the resolved provider secret into the config:\n%s", body)
	}
	if !strings.Contains(body, "${ORCH_TEST_OPENROUTER_KEY}") {
		t.Errorf("Save lost the provider reference:\n%s", body)
	}
}

// Setting a variable for every terminal on Windows means setx and a new
// shell; a file next to the config is what makes this usable day to day.
func TestLoad_ReadsVariablesFromTheOrchestraEnvFile(t *testing.T) {
	path := writeMinimalConfig(t, minimalConfig+"  api_key: ${ORCH_TEST_FILE_KEY}\n")
	writeEnvFile(t, path, "# keys for this project\nORCH_TEST_FILE_KEY=sk-from-the-file\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKey != "sk-from-the-file" {
		t.Errorf("api_key = %q, want the value from .orchestra.env", cfg.LLM.APIKey)
	}
}

// The real environment is the override: a key exported for one run must beat
// whatever the file says, the way every other tool treats a dotenv file.
func TestLoad_TheProcessEnvironmentBeatsTheFile(t *testing.T) {
	t.Setenv("ORCH_TEST_FILE_KEY", "sk-from-the-process")
	path := writeMinimalConfig(t, minimalConfig+"  api_key: ${ORCH_TEST_FILE_KEY}\n")
	writeEnvFile(t, path, "ORCH_TEST_FILE_KEY=sk-from-the-file\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKey != "sk-from-the-process" {
		t.Errorf("api_key = %q, want the exported value to win", cfg.LLM.APIKey)
	}
}

// The file holds secrets in plain text: it must not follow the config into a
// commit, and it is written for its owner only.
func TestOrchestraEnvFile_IsNotReadableByOthers(t *testing.T) {
	path := writeMinimalConfig(t, minimalConfig)
	writeEnvFile(t, path, "ORCH_TEST_FILE_KEY=sk\n")

	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	// Loading must not create, move or rewrite the file.
	body, err := os.ReadFile(filepath.Join(filepath.Dir(path), EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ORCH_TEST_FILE_KEY=sk\n" {
		t.Errorf(".orchestra.env was rewritten by Load: %q", string(body))
	}
}

func writeEnvFile(t *testing.T, cfgPath, body string) {
	t.Helper()
	p := filepath.Join(filepath.Dir(cfgPath), EnvFileName)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
