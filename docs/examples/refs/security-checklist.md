<security-checklist>
Adapted from OWASP Top 10 + common Go/web service pitfalls. For each finding cite `file:line` and severity (`critical` / `high` / `medium` / `low`).

**Injection**
- SQL: any string concatenation into queries → use parameterised queries / placeholders.
- Command: any `os/exec` argument built from user input → use `exec.Command` with fixed args, never `sh -c`.
- Path traversal: any file path derived from user input → resolve + verify it stays under an allow-list root.
- Template: HTML/text templates rendering untrusted strings without escaping.

**Authentication / Authorization**
- Session tokens stored insecurely (plaintext, localStorage for sensitive, no httpOnly/secure flags).
- Missing auth check on a protected endpoint — grep handler list, ensure each has middleware.
- Authz check based on data sent by client (`isAdmin` in request body) instead of server-side state.
- Time-of-check vs time-of-use races on permission checks.

**Cryptography**
- `math/rand` used where `crypto/rand` is required (tokens, IDs, nonces).
- Hard-coded keys, secrets, or credentials anywhere in source.
- Weak hash for passwords (MD5, SHA1, plain SHA256) — must be bcrypt/argon2/scrypt.
- TLS verification disabled (`InsecureSkipVerify: true`).

**Data Exposure**
- PII / secrets logged at any level (search log calls for `password`, `token`, `email`).
- Stack traces / internal errors returned to clients.
- Backup files / `.env` / `.git` accessible from public paths.

**Input Validation**
- Numeric bounds not enforced (could overflow, allocate huge buffers).
- File upload size / type unchecked.
- SSRF: server fetches user-provided URL without allow-listing scheme/host.
- XXE / unsafe deserialization (YAML/JSON tags on unintended fields).

**Dependencies**
- Pinned to versions with known CVEs (check `go list -m -json all` against advisory DB if available).
- Indirect deps that look unusual / typosquats.

**Project-specific (Orchestra)**
- Tools that escape project_root, especially after symlink resolution.
- Op appliers that skip `file_hash` re-check before writing.
- JSON-RPC handlers that accept `allowExec` / `allowWeb` from request params rather than config.
- Hooks that exec arbitrary shell from agent output.
</security-checklist>
