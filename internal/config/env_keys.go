package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// EnvFileName is the per-project file holding credentials as KEY=VALUE lines.
//
// A key belongs in the environment, not in .orchestra.yml: that file is
// committed, shared between every frontend, and rewritten by the UI whenever a
// setting changes. So the config names a variable —
//
//	api_key: ${GEMINI_API_KEY}
//
// — and the value comes from the process environment, or from this file when
// the environment does not carry it. Load only ever reads it; the file is
// gitignored and the reference is what Save writes back.
const EnvFileName = ".orchestra.env"

// envRefPattern matches a ${NAME} reference. Anything else — a literal key, a
// URL with a dollar in it — is left exactly as written.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// envRef remembers one expansion: the text the file spelled and the value it
// resolved to, so Save can tell an untouched reference from a value the user
// has since changed in the UI.
type envRef struct {
	literal  string
	resolved string
}

// expandEnvRefs resolves ${NAME} in every credential field. An unset variable
// expands to nothing rather than staying as "${NAME}": that string would go
// out as the bearer token and come back as an opaque 401.
func (c *ProjectConfig) expandEnvRefs(dir string) {
	lookup := envLookup(dir)
	expand := func(key, in string) string {
		if !strings.Contains(in, "${") {
			return in
		}
		out := envRefPattern.ReplaceAllStringFunc(in, func(m string) string {
			return lookup(envRefPattern.FindStringSubmatch(m)[1])
		})
		if c.envRefs == nil {
			c.envRefs = map[string]envRef{}
		}
		c.envRefs[key] = envRef{literal: in, resolved: out}
		return out
	}

	c.LLM.APIKey = expand("llm.api_key", c.LLM.APIKey)
	c.LLM.APIBase = expand("llm.api_base", c.LLM.APIBase)
	c.Embed.APIKey = expand("embed.api_key", c.Embed.APIKey)
	c.Embed.APIBase = expand("embed.api_base", c.Embed.APIBase)
	for name, p := range c.Providers {
		p.APIKey = expand("providers."+name+".api_key", p.APIKey)
		p.APIBase = expand("providers."+name+".api_base", p.APIBase)
		c.Providers[name] = p
	}
}

// restoreEnvRefs puts each ${NAME} back in place of the value it resolved to,
// on the copy of the config that is about to be written. A field the user has
// changed since Load no longer matches what was resolved and keeps its new
// value: only an untouched expansion is restored.
func (c *ProjectConfig) restoreEnvRefs() {
	if len(c.envRefs) == 0 {
		return
	}
	restore := func(key, cur string) string {
		ref, ok := c.envRefs[key]
		if !ok || cur != ref.resolved {
			return cur
		}
		return ref.literal
	}

	c.LLM.APIKey = restore("llm.api_key", c.LLM.APIKey)
	c.LLM.APIBase = restore("llm.api_base", c.LLM.APIBase)
	c.Embed.APIKey = restore("embed.api_key", c.Embed.APIKey)
	c.Embed.APIBase = restore("embed.api_base", c.Embed.APIBase)
	if len(c.Providers) == 0 {
		return
	}
	// The caller holds a shallow copy of the config, so the providers map is
	// still the loaded one: writing through it here would replace the running
	// config's live keys with their references.
	providers := make(map[string]LLMConfig, len(c.Providers))
	for name, p := range c.Providers {
		p.APIKey = restore("providers."+name+".api_key", p.APIKey)
		p.APIBase = restore("providers."+name+".api_base", p.APIBase)
		providers[name] = p
	}
	c.Providers = providers
}

// envLookup resolves a variable name against the process environment first and
// the project's .orchestra.env second: a value exported for one run overrides
// the file, the way every other tool treats a dotenv file.
func envLookup(dir string) func(string) string {
	file := readEnvFile(filepath.Join(dir, EnvFileName))
	return func(name string) string {
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return file[name]
	}
}

// readEnvFile parses KEY=VALUE lines. A missing or unreadable file is not an
// error: the environment may well carry everything the config asks for.
func readEnvFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = unquoteEnvValue(strings.TrimSpace(value))
	}
	return out
}

// EnvVarFor returns the variable the loaded file named for a credential
// field ("llm.api_key", "providers.gemini.api_key"), or "" when the file
// spelled a literal there.
//
// Reading the field itself is not enough: by the time anyone sees the config,
// Load has already replaced ${NAME} with the value — with nothing at all when
// the variable is unset — so the name survives only here.
func (c *ProjectConfig) EnvVarFor(field string) string {
	return EnvRefName(c.envRefs[field].literal)
}

// EnvRefName returns the variable a credential field names, or "" when the
// field holds a literal. Callers storing a key reuse the name the config
// already refers to instead of inventing a second one for the same field.
func EnvRefName(value string) string {
	m := envRefPattern.FindStringSubmatch(value)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// UpsertEnvVar writes name=value into the project's .orchestra.env, replacing
// the variable's line if it is already there and leaving every other line —
// comments and other keys included — exactly as it found it.
//
// The file is created 0600: it holds secrets in plain text, and it is the
// place they go instead of the committed config.
func UpsertEnvVar(dir, name, value string) error {
	path := filepath.Join(dir, EnvFileName)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", EnvFileName, err)
	}

	line := name + "=" + value
	assign := regexp.MustCompile(`^\s*(export\s+)?` + regexp.QuoteMeta(name) + `\s*=`)
	var out []string
	replaced := false
	if len(existing) > 0 {
		for _, l := range strings.Split(strings.ReplaceAll(string(existing), "\r\n", "\n"), "\n") {
			if assign.MatchString(l) {
				if replaced {
					continue // a duplicate of the key we just rewrote
				}
				out, replaced = append(out, line), true
				continue
			}
			out = append(out, l)
		}
		// Split leaves a trailing empty element for a file ending in a
		// newline; drop it so the join below does not double it.
		if n := len(out); n > 0 && out[n-1] == "" {
			out = out[:n-1]
		}
	}
	if !replaced {
		out = append(out, line)
	}

	body := strings.Join(out, "\n") + "\n"
	if err := fsutil.AtomicWriteFile(path, []byte(body), 0600); err != nil {
		return fmt.Errorf("write %s: %w", EnvFileName, err)
	}
	return nil
}

// unquoteEnvValue drops one matching pair of surrounding quotes, so a key
// pasted with them is not sent with them.
func unquoteEnvValue(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
