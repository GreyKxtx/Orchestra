package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// RoutingFileName is the tier→model binding file loaded from the project root
// (next to .orchestra.yml). See docs/architecture/orchestra-routing.md §7.3.
const RoutingFileName = "orchestra_routing.yaml"

// RoutingRole binds one AI tier (L1–L5, optionally specialized like L4_product)
// to a provider/model. Agents address tiers; only bindings change on model churn.
type RoutingRole struct {
	Label        string `yaml:"label,omitempty"`
	Provider     string `yaml:"provider,omitempty"` // named providers: entry
	Model        string `yaml:"model,omitempty"`
	SubagentType string `yaml:"subagent_type,omitempty"`
}

// RoutingRule maps a task_type to a required tier and default spawn parameters.
type RoutingRule struct {
	RequiredTier string `yaml:"required_tier"`
	SubagentType string `yaml:"subagent_type,omitempty"`
	// Tier is the legacy worker band (complex|focused|micro) used when
	// SubagentType is "worker" and orchestra.tiers bindings exist.
	Tier string `yaml:"tier,omitempty"`
}

// OrchestraRouting is the parsed orchestra_routing.yaml.
type OrchestraRouting struct {
	Version   int                    `yaml:"version"`
	Roles     map[string]RoutingRole `yaml:"roles,omitempty"`
	LegacyMap map[string]string      `yaml:"legacy_map,omitempty"`
	Routing   map[string]RoutingRule `yaml:"routing,omitempty"`
}

// defaultLegacyMap maps pre-tier names (orchestra.tiers / planner) to L1–L5.
var defaultLegacyMap = map[string]string{
	"planner": "L5",
	"lead":    "L4",
	"complex": "L3",
	"focused": "L3",
	"micro":   "L1",
	"explore": "L2",
}

// tierKeyRe matches role keys: a base tier L1–L5 with an optional
// specialization suffix (e.g. L4_product, L4_docs).
var tierKeyRe = regexp.MustCompile(`^L[1-5](_[a-z0-9_]+)?$`)

// IsTierName reports whether name is an L1–L5 role key (base or specialized).
func IsTierName(name string) bool {
	return tierKeyRe.MatchString(strings.TrimSpace(name))
}

// LoadOrchestraRouting reads RoutingFileName from dir.
// A missing file is not an error and yields (nil, nil).
func LoadOrchestraRouting(dir string) (*OrchestraRouting, error) {
	path := filepath.Join(dir, RoutingFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", RoutingFileName, err)
	}
	var r OrchestraRouting
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", RoutingFileName, err)
	}
	return &r, nil
}

// Validate checks role keys, provider references and routing rules.
// providers may be nil to skip provider-name checks.
func (r *OrchestraRouting) Validate(providers map[string]LLMConfig) error {
	if r == nil {
		return nil
	}
	if r.Version != 0 && r.Version != 1 {
		return fmt.Errorf("%s: unsupported version %d (want 1)", RoutingFileName, r.Version)
	}
	for key, role := range r.Roles {
		k := strings.TrimSpace(key)
		if !tierKeyRe.MatchString(k) {
			return fmt.Errorf("%s: roles key %q must match L1–L5 with optional suffix (e.g. L4_product)", RoutingFileName, key)
		}
		if p := strings.TrimSpace(role.Provider); p != "" && providers != nil {
			if _, ok := providers[p]; !ok {
				return fmt.Errorf("%s: roles.%s.provider %q not defined in providers", RoutingFileName, key, p)
			}
		}
	}
	for legacy, tier := range r.LegacyMap {
		if !tierKeyRe.MatchString(strings.TrimSpace(tier)) {
			return fmt.Errorf("%s: legacy_map.%s → %q is not a valid tier", RoutingFileName, legacy, tier)
		}
	}
	for taskType, rule := range r.Routing {
		if strings.TrimSpace(taskType) == "" {
			return fmt.Errorf("%s: routing has an empty task_type key", RoutingFileName)
		}
		if !tierKeyRe.MatchString(strings.TrimSpace(rule.RequiredTier)) {
			return fmt.Errorf("%s: routing.%s.required_tier %q is not a valid tier (L1–L5)", RoutingFileName, taskType, rule.RequiredTier)
		}
	}
	return nil
}

// MapLegacy translates a legacy tier name (planner|complex|focused|micro|explore)
// to an L1–L5 tier using the file's legacy_map with built-in defaults.
// Names already in L-form pass through; unknown names return "".
func (r *OrchestraRouting) MapLegacy(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return ""
	}
	if tierKeyRe.MatchString(n) {
		return n
	}
	lower := strings.ToLower(n)
	if r != nil {
		for legacy, tier := range r.LegacyMap {
			if strings.EqualFold(strings.TrimSpace(legacy), lower) {
				return strings.TrimSpace(tier)
			}
		}
	}
	return defaultLegacyMap[lower]
}

// ResolveRole returns the binding for a tier key. Specialized roles fall back
// to their base tier: L4_product → L4 when L4_product is not defined.
func (r *OrchestraRouting) ResolveRole(tier string) (RoutingRole, bool) {
	if r == nil {
		return RoutingRole{}, false
	}
	want := strings.TrimSpace(tier)
	if want == "" {
		return RoutingRole{}, false
	}
	if role, ok := r.findRole(want); ok {
		return role, true
	}
	if i := strings.IndexByte(want, '_'); i > 0 {
		return r.findRole(want[:i])
	}
	return RoutingRole{}, false
}

func (r *OrchestraRouting) findRole(key string) (RoutingRole, bool) {
	for k, role := range r.Roles {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return role, true
		}
	}
	return RoutingRole{}, false
}

// Route returns the routing rule for a task_type (case-insensitive).
func (r *OrchestraRouting) Route(taskType string) (RoutingRule, bool) {
	if r == nil {
		return RoutingRule{}, false
	}
	want := strings.TrimSpace(taskType)
	if want == "" {
		return RoutingRule{}, false
	}
	for k, rule := range r.Routing {
		if strings.EqualFold(strings.TrimSpace(k), want) {
			return rule, true
		}
	}
	return RoutingRule{}, false
}

// ResolveTierBinding maps a tier name to provider/model. Precedence:
//  1. orchestra.tiers exact match (legacy behavior, backward compatible);
//  2. orchestra_routing.yaml roles — direct L-name or via legacy_map.
//
// Returns ok=false when neither source has a binding.
func (c *ProjectConfig) ResolveTierBinding(name string) (provider, model string, ok bool) {
	if c == nil {
		return "", "", false
	}
	trimmed := strings.TrimSpace(name)
	// Legacy bands keep resolving through orchestra.tiers first so existing
	// .orchestra.yml configs behave identically (including default_tier for "").
	if !IsTierName(trimmed) {
		if t := c.Orchestra.FindTier(trimmed); t != nil && (strings.TrimSpace(t.Provider) != "" || strings.TrimSpace(t.Model) != "") {
			return strings.TrimSpace(t.Provider), strings.TrimSpace(t.Model), true
		}
	}
	if c.Routing == nil {
		return "", "", false
	}
	tier := c.Routing.MapLegacy(trimmed)
	if tier == "" && trimmed == "" {
		tier = c.Routing.MapLegacy(c.Orchestra.ResolvedDefaultTier())
	}
	if tier == "" {
		return "", "", false
	}
	role, found := c.Routing.ResolveRole(tier)
	if !found {
		return "", "", false
	}
	p := strings.TrimSpace(role.Provider)
	m := strings.TrimSpace(role.Model)
	if p == "" && m == "" {
		return "", "", false
	}
	return p, m, true
}
