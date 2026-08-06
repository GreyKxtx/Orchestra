package guard

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
)

// matchSubject reports whether pattern matches subject.
//
// For file-path subjects (write, edit, read, ls, grep, symbols, glob tool),
// path.Match semantics are used: '*' matches any non-'/' sequence.
// For other subjects (bash commands, URLs, explore names), simple wildcard
// matching is used: '*' matches any sequence including '/'.
func matchSubject(pattern, subject string, isPathSubject bool) bool {
	if pattern == "*" {
		return true
	}
	if isPathSubject {
		ok, _ := path.Match(pattern, subject)
		return ok
	}
	return matchSimpleGlob(pattern, subject)
}

// matchSimpleGlob matches pattern against s where '*' matches any sequence
// of any characters (including '/').
func matchSimpleGlob(pattern, s string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, part := range parts {
		switch {
		case i == 0:
			if !strings.HasPrefix(s[pos:], part) {
				return false
			}
			pos += len(part)
		case i == len(parts)-1:
			return strings.HasSuffix(s[pos:], part)
		default:
			idx := strings.Index(s[pos:], part)
			if idx < 0 {
				return false
			}
			pos += idx + len(part)
		}
	}
	return true
}

// subjectForTool returns the string that permission rule patterns are matched
// against for the given tool name.
//
// Subject table (tool names are the short canonical forms used throughout the
// agent loop, e.g. "bash", "read", "webfetch"):
//
//	bash              → command string
//	webfetch          → URL
//	write, edit,
//	read, ls, grep,
//	symbols           → path argument
//	glob              → pattern argument
//	explore           → name argument
//	(all others)      → "" (rules can still match by tool name with no pattern)
func SubjectForTool(name string, input json.RawMessage) string {
	key := subjectKey(name)
	if key == "" {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil || m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func subjectKey(name string) string {
	switch name {
	case "bash":
		return "command"
	case "webfetch":
		return "url"
	case "write", "edit", "read", "ls", "grep", "symbols":
		return "path"
	case "glob":
		return "pattern"
	case "explore":
		return "name"
	}
	return ""
}

// checkPermissions evaluates the ordered ruleset against the resolved tool name
// and its subject. First matching rule wins.
//
// Returns action ("allow" or "deny") and matched=true when a rule fires.
// Returns matched=false when no rule matches → caller falls through to existing
// consent gates (no change in behaviour).
//
// A rule matches when:
//   - rule.Tool == "*"  OR  rule.Tool matches name (case-insensitive)
//   - rule.Pattern == ""  OR  path.Match(rule.Pattern, subject) is true
func CheckPermissions(rules []config.PermissionRule, name, subject string) (action string, matched bool) {
	isPath := subjectKey(name) == "path" || subjectKey(name) == "pattern"
	for _, r := range rules {
		if !ruleToolMatches(r.Tool, name) {
			continue
		}
		if r.Pattern != "" {
			if !matchSubject(r.Pattern, subject, isPath) {
				continue
			}
		}
		act := strings.ToLower(strings.TrimSpace(r.Action))
		if act != "allow" && act != "deny" {
			continue // skip malformed rules silently
		}
		return act, true
	}
	return "", false
}

// ruleToolMatches reports whether the rule's Tool field applies to the given
// resolved tool name. Accepts glob patterns via `*` (matches any sequence
// including `:` and `.` so `mcp:server:*`, `mcp:*`, `git.*`, `browser.*`
// all behave as expected). Case-insensitive on both sides.
//
// C7 in audit ledger: previously only the literal `*` token was a wildcard;
// the user had no way to deny a whole MCP server's tools or every git.*
// command short of spelling each name. Now full glob (via matchSimpleGlob,
// the same engine bash/url/explore subjects use).
func ruleToolMatches(ruleTool, name string) bool {
	rt := strings.ToLower(strings.TrimSpace(ruleTool))
	n := strings.ToLower(strings.TrimSpace(name))
	if rt == "*" {
		return true
	}
	if !strings.Contains(rt, "*") {
		return rt == n
	}
	return matchSimpleGlob(rt, n)
}
