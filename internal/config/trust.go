package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
	"gopkg.in/yaml.v3"
)

// Workspace trust.
//
// .orchestra.yml is committed, so it comes with every clone — and some of
// its settings act on the machine that opens the project: MCP servers and
// hooks start processes, lsp.servers names language-server commands,
// exec.confirm: false and allow rules let the model run commands unasked,
// an auth block runs a token command, and an api_base decides where the
// user's API key is sent. Opening a hostile repository therefore ran its
// code and could hand the user's key to its endpoint.
//
// Those settings take effect only in a trusted workspace: one whose current
// "dangerous slice" — exactly those settings, from .orchestra.yml, the
// .orchestra.local.yml next to it and .mcp.json — has been recorded in
// ~/.orchestra/trusted-workspaces.json. Until then Load leaves them out and
// says which (ProjectConfig.Trust). A change to the slice — a pull that adds
// a hook, an agent that edits the config — needs trusting again. The user's
// own ~/.orchestra/config.yml is always in effect; `security:
// workspace_trust: off` there turns the check off.
//
// Trust is recorded by `orchestra trust`, the workspace.trust RPC, `orchestra
// init`, and by Save when the slice it replaces was already trusted: a
// setting the user changes through Orchestra is theirs.

// WorkspaceTrust reports how Load treated the project's own settings.
type WorkspaceTrust struct {
	// Enforced is false when ~/.orchestra/config.yml turns the check off.
	Enforced bool `json:"enforced"`
	// Trusted means the project's settings are in effect as written.
	Trusted bool `json:"trusted"`
	// Ignored names the settings Load left out because the workspace is not
	// trusted, e.g. "mcp.servers", "hooks", "llm.api_key (api_base set by the
	// project)".
	Ignored []string `json:"ignored,omitempty"`
	// Hash fingerprints the dangerous slice; empty when there is none.
	Hash string `json:"hash,omitempty"`
}

// TrustStoreName is the file under ~/.orchestra/ that lists trusted
// workspaces.
const TrustStoreName = "trusted-workspaces.json"

type trustStore struct {
	Version    int                     `json:"version"`
	Workspaces map[string]trustedEntry `json:"workspaces"`
}

type trustedEntry struct {
	Hash      string `json:"hash"`
	TrustedAt string `json:"trusted_at"`
}

func trustStorePath() string {
	if p := strings.TrimSpace(os.Getenv("ORCHESTRA_TRUST_STORE")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".orchestra", TrustStoreName)
}

func readTrustStore() trustStore {
	st := trustStore{Version: 1, Workspaces: map[string]trustedEntry{}}
	p := trustStorePath()
	if p == "" {
		return st
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return st
	}
	var got trustStore
	if json.Unmarshal(data, &got) == nil && got.Workspaces != nil {
		got.Version = 1
		return got
	}
	return st
}

func writeTrustStore(st trustStore) error {
	p := trustStorePath()
	if p == "" {
		return fmt.Errorf("workspace trust: no home directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(p, append(data, '\n'), 0o600)
}

// workspaceKey is the trust store's key for the project that configPath
// belongs to: its directory, absolute, symlinks resolved.
func workspaceKey(configPath string) string {
	dir, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		dir = filepath.Dir(configPath)
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return dir
}

// trustEnforced reads security.workspace_trust from the user-level config,
// or ORCHESTRA_WORKSPACE_TRUST=off from the environment (containers, CI):
// both belong to the user, never to the project, which cannot vouch for
// itself.
func trustEnforced() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ORCHESTRA_WORKSPACE_TRUST")), "off") {
		return false
	}
	m, err := readGlobalConfigMap()
	if err != nil || m == nil {
		return true
	}
	sec, _ := m["security"].(map[string]any)
	v, _ := sec["workspace_trust"].(string)
	return !strings.EqualFold(strings.TrimSpace(v), "off")
}

// TrustWorkspace records the current dangerous slice of the project at
// configPath as trusted.
func TrustWorkspace(configPath string) (WorkspaceTrust, error) {
	g, err := evaluateWorkspaceTrust(configPath)
	if err != nil {
		return WorkspaceTrust{}, err
	}
	if g.trust.Hash == "" {
		g.trust.Trusted = true
		return g.trust, nil
	}
	st := readTrustStore()
	st.Workspaces[workspaceKey(configPath)] = trustedEntry{Hash: g.trust.Hash, TrustedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := writeTrustStore(st); err != nil {
		return WorkspaceTrust{}, fmt.Errorf("workspace trust: %w", err)
	}
	g.trust.Trusted = true
	g.trust.Ignored = nil
	return g.trust, nil
}

// UntrustWorkspace forgets the project at configPath.
func UntrustWorkspace(configPath string) error {
	st := readTrustStore()
	key := workspaceKey(configPath)
	if _, ok := st.Workspaces[key]; !ok {
		return nil
	}
	delete(st.Workspaces, key)
	return writeTrustStore(st)
}

// WorkspaceTrustStatus reports whether the project at configPath is trusted
// and, if not, which of its settings Load would leave out.
func WorkspaceTrustStatus(configPath string) (WorkspaceTrust, error) {
	g, err := evaluateWorkspaceTrust(configPath)
	return g.trust, err
}

// trustGate is Load's decision for one project.
type trustGate struct {
	trust WorkspaceTrust
	// isolated lists the model-endpoint blocks ("llm", "embed",
	// "providers.<name>") whose api_base the untrusted layers set, with the
	// literal key those layers gave (possibly ""): the only key such an
	// endpoint may receive.
	isolated map[string]string
}

// evaluateWorkspaceTrust reads the project's layers and decides.
func evaluateWorkspaceTrust(configPath string) (trustGate, error) {
	g := trustGate{trust: WorkspaceTrust{Enforced: trustEnforced()}}
	project, err := readYAMLMap(configPath)
	if err != nil {
		return g, err
	}
	local, err := readYAMLMap(localOverlayPath(configPath))
	if err != nil {
		return g, err
	}
	mcpJSON, _ := os.ReadFile(filepath.Join(filepath.Dir(configPath), MCPJSONName))
	global, _ := readGlobalConfigMap()

	slice := map[string]any{}
	for name, layer := range map[string]map[string]any{"project": project, "local": local} {
		if s := dangerousSlice(layer, global); len(s) > 0 {
			slice[name] = s
		}
	}
	if len(mcpJSON) > 0 {
		slice["mcp_json"] = string(mcpJSON)
	}
	if len(slice) == 0 {
		g.trust.Trusted = true
		return g, nil
	}
	canon, err := json.Marshal(slice) // map keys marshal sorted
	if err != nil {
		return g, err
	}
	sum := sha256.Sum256(canon)
	g.trust.Hash = hex.EncodeToString(sum[:])

	if !g.trust.Enforced {
		g.trust.Trusted = true
		return g, nil
	}
	if e, ok := readTrustStore().Workspaces[workspaceKey(configPath)]; ok && e.Hash == g.trust.Hash {
		g.trust.Trusted = true
		return g, nil
	}

	seen := map[string]bool{}
	for _, layer := range []map[string]any{project, local} {
		for _, name := range ignoredNames(layer, global) {
			if !seen[name] {
				seen[name] = true
				g.trust.Ignored = append(g.trust.Ignored, name)
			}
		}
	}
	if len(mcpJSON) > 0 {
		g.trust.Ignored = append(g.trust.Ignored, MCPJSONName)
	}
	sort.Strings(g.trust.Ignored)
	g.isolated = map[string]string{}
	for _, layer := range []map[string]any{project, local} { // local wins, as when merging
		for block, key := range endpointKeys(layer, global) {
			g.isolated[block] = key
		}
	}
	return g, nil
}

func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", filepath.Base(path), err)
	}
	return m, nil
}

// dangerousLeaves are the settings that act on the machine by themselves.
// "*" matches every key of a mapping.
var dangerousLeaves = [][]string{
	{"mcp", "servers"},
	{"hooks"},
	{"exec", "confirm"}, {"exec", "allow"}, {"exec", "env_passthrough"},
	{"web", "confirm"},
	{"lsp", "servers"},
	{"llm", "auth"}, {"providers", "*", "auth"},
}

// endpointBlocks are the model-endpoint settings: their api_base decides
// where a key goes.
var endpointBlocks = [][]string{{"llm"}, {"embed"}, {"providers", "*"}}

// dangerousSlice collects a layer's dangerous settings for the fingerprint.
// global is the user's own config, for the keys an endpoint would inherit.
func dangerousSlice(m, global map[string]any) map[string]any {
	out := map[string]any{}
	for _, pat := range dangerousLeaves {
		for _, p := range expandPath(m, pat) {
			if v, ok := getPath(m, p); ok {
				out[strings.Join(p, ".")] = v
			}
		}
	}
	if rules := allowRules(m); len(rules) > 0 {
		out["permissions.rules[allow]"] = rules
	}
	for _, pat := range endpointBlocks {
		for _, p := range expandPath(m, pat) {
			if risky, _ := endpointRisk(m, global, p); !risky {
				continue
			}
			bm := blockMap(m, p)
			name := strings.Join(p, ".")
			out[name+".api_base"] = bm["api_base"]
			out[name+".api_key"] = bm["api_key"]
		}
	}
	return out
}

// endpointRisk reports whether the layer points block p's key somewhere the
// user did not: it sends a ${VAR} from the user's environment (in api_key or
// api_base), or it sets api_base while the key would come from the user's
// ~/.orchestra/config.yml. A project that names its own endpoint and gives no
// key, or a literal key of its own, risks nothing of the user's — that is how
// .orchestra.yml normally looks (a local LM Studio base, say). literalKey is
// the key the layer wrote itself ("" for none or a reference).
func endpointRisk(m, global map[string]any, p []string) (risky bool, literalKey string) {
	bm := blockMap(m, p)
	if bm == nil {
		return false, ""
	}
	key, _ := bm["api_key"].(string)
	base, hasBase := bm["api_base"]
	baseStr, _ := base.(string)
	literalKey = key
	if strings.Contains(key, "${") {
		literalKey = ""
		risky = true
	}
	if strings.Contains(baseStr, "${") {
		risky = true
	}
	if hasBase && key == "" {
		if gk, _ := blockMap(global, p)["api_key"].(string); strings.TrimSpace(gk) != "" {
			risky = true
		}
	}
	return risky, literalKey
}

func blockMap(m map[string]any, p []string) map[string]any {
	v, _ := getPath(m, p)
	bm, _ := v.(map[string]any)
	return bm
}

// ignoredNames is what stripUntrusted removes from a layer, for the report.
func ignoredNames(m, global map[string]any) []string {
	var out []string
	for _, pat := range dangerousLeaves {
		for _, p := range expandPath(m, pat) {
			if _, ok := getPath(m, p); ok {
				out = append(out, strings.Join(p, "."))
			}
		}
	}
	if len(allowRules(m)) > 0 {
		out = append(out, "permissions.rules (allow)")
	}
	for block := range endpointKeys(m, global) {
		out = append(out, block+".api_key (the project chose where it goes)")
	}
	return out
}

// endpointKeys maps each endpoint block the layer puts at risk
// (endpointRisk) to the literal key it wrote: the only key that endpoint may
// receive while the workspace is untrusted.
func endpointKeys(m, global map[string]any) map[string]string {
	out := map[string]string{}
	for _, pat := range endpointBlocks {
		for _, p := range expandPath(m, pat) {
			if risky, key := endpointRisk(m, global, p); risky {
				out[strings.Join(p, ".")] = key
			}
		}
	}
	return out
}

// stripUntrusted removes a layer's dangerous settings in place and returns
// the paths it removed. Allow rules go; ask and deny rules stay. An endpoint
// block keeps its api_base but loses ${VAR} references in api_base and
// api_key.
func stripUntrusted(m map[string]any) [][]string {
	var removed [][]string
	for _, pat := range dangerousLeaves {
		for _, p := range expandPath(m, pat) {
			if deletePath(m, p) {
				removed = append(removed, p)
			}
		}
	}
	if perm, ok := m["permissions"].(map[string]any); ok {
		if rules, ok := perm["rules"].([]any); ok {
			kept := make([]any, 0, len(rules))
			for _, r := range rules {
				if !isAllowRule(r) {
					kept = append(kept, r)
				}
			}
			if len(kept) != len(rules) {
				perm["rules"] = kept
				removed = append(removed, []string{"permissions", "rules"})
			}
		}
	}
	for _, pat := range endpointBlocks {
		for _, p := range expandPath(m, pat) {
			bm := blockMap(m, p)
			for _, field := range []string{"api_base", "api_key"} {
				if s, ok := bm[field].(string); ok && strings.Contains(s, "${") {
					delete(bm, field)
					removed = append(removed, append(append([]string(nil), p...), field))
				}
			}
		}
	}
	return removed
}

func allowRules(m map[string]any) []any {
	perm, _ := m["permissions"].(map[string]any)
	rules, _ := perm["rules"].([]any)
	var out []any
	for _, r := range rules {
		if isAllowRule(r) {
			out = append(out, r)
		}
	}
	return out
}

func isAllowRule(r any) bool {
	rm, _ := r.(map[string]any)
	act, _ := rm["action"].(string)
	return strings.EqualFold(strings.TrimSpace(act), "allow")
}

// expandPath resolves "*" segments of pat against m.
func expandPath(m map[string]any, pat []string) [][]string {
	var out [][]string
	var walk func(cur map[string]any, i int, prefix []string)
	walk = func(cur map[string]any, i int, prefix []string) {
		if cur == nil {
			return
		}
		if i == len(pat)-1 {
			if pat[i] == "*" {
				for k := range cur {
					out = append(out, append(append([]string(nil), prefix...), k))
				}
			} else if _, ok := cur[pat[i]]; ok {
				out = append(out, append(append([]string(nil), prefix...), pat[i]))
			}
			return
		}
		if pat[i] == "*" {
			for k, v := range cur {
				sub, _ := v.(map[string]any)
				walk(sub, i+1, append(append([]string(nil), prefix...), k))
			}
			return
		}
		sub, _ := cur[pat[i]].(map[string]any)
		walk(sub, i+1, append(append([]string(nil), prefix...), pat[i]))
	}
	walk(m, 0, nil)
	sort.Slice(out, func(a, b int) bool { return strings.Join(out[a], ".") < strings.Join(out[b], ".") })
	return out
}

func getPath(m map[string]any, p []string) (any, bool) {
	var cur any = m
	for _, k := range p {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = cm[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func deletePath(m map[string]any, p []string) bool {
	if len(p) == 0 {
		return false
	}
	parent, ok := getPath(m, p[:len(p)-1])
	if len(p) == 1 {
		parent, ok = m, true
	}
	pm, isMap := parent.(map[string]any)
	if !ok || !isMap {
		return false
	}
	if _, ok := pm[p[len(p)-1]]; !ok {
		return false
	}
	delete(pm, p[len(p)-1])
	return true
}

// setPath sets p in m, creating mappings on the way.
func setPath(m map[string]any, p []string, v any) {
	cur := m
	for _, k := range p[:len(p)-1] {
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	cur[p[len(p)-1]] = v
}

// isolateCredentials gives each endpoint the untrusted layers pointed
// somewhere only the key those layers wrote literally — never one inherited
// from ~/.orchestra/config.yml or read from the environment. Runs before
// ${VAR} expansion.
func (c *ProjectConfig) isolateCredentials(isolated map[string]string) {
	for block, key := range isolated {
		switch {
		case block == "llm":
			c.LLM.APIKey = key
			c.LLM.Auth = nil
		case block == "embed":
			c.Embed.APIKey = key
		case strings.HasPrefix(block, "providers."):
			name := strings.TrimPrefix(block, "providers.")
			if p, ok := c.Providers[name]; ok {
				p.APIKey = key
				p.Auth = nil
				c.Providers[name] = p
			}
		}
	}
}

// restoreUntrustedLeaves puts the settings Load left out of an untrusted
// project back into data (the config about to be saved) as they are on disk,
// so saving a setting does not delete the project's MCP servers or hooks.
func restoreUntrustedLeaves(configPath string, data []byte, leaves [][]string) ([]byte, error) {
	if len(leaves) == 0 {
		return data, nil
	}
	disk, err := readYAMLMap(configPath)
	if err != nil {
		return nil, err
	}
	var next map[string]any
	if err := yaml.Unmarshal(data, &next); err != nil {
		return nil, fmt.Errorf("restore untrusted settings: %w", err)
	}
	if next == nil {
		next = map[string]any{}
	}
	for _, p := range leaves {
		if v, ok := getPath(disk, p); ok {
			setPath(next, p, v)
		}
	}
	return yaml.Marshal(next)
}

// stripUntrustedYAML is stripUntrusted on a layer's bytes.
func stripUntrustedYAML(data []byte) ([]byte, [][]string, error) {
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	if m == nil {
		return data, nil, nil
	}
	removed := stripUntrusted(m)
	if len(removed) == 0 {
		return data, nil, nil
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		return nil, nil, err
	}
	return out, removed, nil
}
