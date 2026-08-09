package agent

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// Named execution profiles (adaptive presets over Options).
const (
	ProfileFast      = "fast"
	ProfilePrecision = "precision"
)

// IsKnownProfile reports whether name is empty or a registered profile.
func IsKnownProfile(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", ProfileFast, ProfilePrecision:
		return true
	default:
		return false
	}
}

// ApplyProfile overlays a named profile onto opts.
// Empty profile is a no-op. Unknown profiles return an error.
//
// Only zero / unset fields that the profile owns are filled when
// preserveNonZero is true — used so CLI/RPC explicit MaxSteps etc. win.
// When preserveNonZero is false, profile values always overwrite.
//
// CustomTools / SystemPromptOverride / provider selection are never
// touched here (named agents: and --provider keep priority).
//
// LLMStepTimeout is intentionally NOT set by profiles — llm.timeout_s in
// .orchestra.yml is the single source of truth (see Core.prepareAgentLaunch).
func ApplyProfile(opts *Options, profile string, preserveNonZero bool) error {
	if opts == nil {
		return fmt.Errorf("agent: ApplyProfile: opts is nil")
	}
	name := strings.ToLower(strings.TrimSpace(profile))
	if name == "" {
		return nil
	}
	if !IsKnownProfile(name) {
		return fmt.Errorf("agent: unknown profile %q (want %q or %q)", profile, ProfileFast, ProfilePrecision)
	}
	opts.Profile = name

	switch name {
	case ProfileFast:
		setInt(&opts.MaxSteps, 10, preserveNonZero)
		setInt(&opts.MaxPromptBytes, 32*1024, preserveNonZero)
		setInt(&opts.MaxInvalidRetries, 2, preserveNonZero)
		setInt(&opts.MaxFinalFailures, 3, preserveNonZero)
		setInt(&opts.MaxToolErrorRepeats, 4, preserveNonZero)
		setInt(&opts.CompactThresholdPct, 60, preserveNonZero)
		opts.AllowBrowser = false
	case ProfilePrecision:
		setInt(&opts.MaxSteps, 36, preserveNonZero)
		setInt(&opts.MaxPromptBytes, 128*1024, preserveNonZero)
		setInt(&opts.MaxInvalidRetries, 5, preserveNonZero)
		setInt(&opts.MaxFinalFailures, 8, preserveNonZero)
		setInt(&opts.MaxToolErrorRepeats, 8, preserveNonZero)
		setInt(&opts.CompactThresholdPct, 75, preserveNonZero)
		if opts.ResponseFormat == nil {
			opts.ResponseFormat = &llm.ResponseFormat{
				Type:       "json_schema",
				Schema:     schema.AgentStepSchemaRaw(),
				SchemaName: "agent_step",
			}
		}
	}
	return nil
}

func setInt(dst *int, v int, preserveNonZero bool) {
	if preserveNonZero && *dst > 0 {
		return
	}
	*dst = v
}
