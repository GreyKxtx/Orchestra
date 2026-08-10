package git

// IsGitSafeRef returns true if s contains only characters valid in a git ref.
func IsGitSafeRef(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' {
		return false
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '/' || c == '-' || c == '_' || c == '.' ||
			c == '~' || c == '^' || c == '@' || c == '{' || c == '}'
		if !ok {
			return false
		}
	}
	return true
}
