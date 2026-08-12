# Security Methodology (L0 default)

**Audience:** Security Lead L4, Security Auditor skill, Pentest Worker L3.  
**Refs:** [security-checklist.md](../refs/security-checklist.md), OWASP Top 10, OWASP ASVS (project level in L1 playbook).

## Algorithm (fixed order — do not skip steps)

### Step 0 — Inputs

- `OpenAPI_Contract.yaml` from Backend (required for API audit)
- `.orchestra/playbooks/security.md` (L1 project overrides)
- Scope from WorkOrder / epic: `whole_repo` | `diff` | `paths[]`

### Step 1 — Attack surface map (Scout L2)

- List: HTTP handlers, CLI entrypoints, file IO, subprocess, MCP/tools, auth middleware chain
- Output: section in `Threat_Model.md` — **Assets → Entry points → Trust boundaries**

### Step 2 — Threat model (Lead L4)

- STRIDE-lite per entry point (Spoofing, Tampering, Repudiation, Info disclosure, DoS, Elevation)
- Prioritize Critical/High attack paths for Step 3

### Step 3 — Static audit (Auditor L4, read-only)

For each bucket in @refs/security-checklist:

1. **Injection** — SQL, command, path, template
2. **AuthN/AuthZ** — missing checks, client-trusted roles
3. **Crypto** — weak RNG, hardcoded secrets, TLS skip
4. **Data exposure** — logs, error leaks, backup paths
5. **Input validation** — bounds, SSRF, upload
6. **Dependencies** — CVE scan (go/npm)
7. **Project-specific** — invariants from L1 playbook `forbidden_patterns`

Tools (non-exhaustive): `grep`, `explore`, gosec, semgrep, npm audit, gitleaks.

**Output:** `Security_Findings.md` with severity + `file:line` + exploit scenario. **No patches.**

### Step 4 — DAST / pentest *(gated)*

Preconditions:

- `pentest_allowed_hosts` from L1 playbook
- `--allow-exec` or sandbox profile
- **Never** prod without explicit User gate

OpenAPI-driven probes: auth bypass, IDOR, injection on documented params.

### Step 5 — Fix planning

- Each finding → finding ID
- WorkOrder references finding ID; executor = BE/FE Worker or Security Fix L3
- Re-audit after fix (Step 3 subset on changed files)

## Severity (calibration)

| Level | Criteria |
|-------|----------|
| critical | Remote unauthenticated RCE, SQLi on public endpoint, auth bypass, secret in source |
| high | Low-priv exploit, sensitive data exfil with auth |
| medium | Limited blast radius, unusual preconditions |
| low | Defense-in-depth gap |
| info | Observation, not a vulnerability |

## L1 project playbook fields

Docs Lead sets in `.orchestra/playbooks/security.md` — use
[security_L1_playbook.template.md](security_L1_playbook.template.md) (contains the
`asvs_level` selection table by `project_profile` and the ASVS ↔ bucket depth mapping):

```yaml
asvs_level: 1 | 2 | 3       # hobby→1, business→2, enterprise→3 (+G5, no waive)
forbidden_patterns: []      # extra project rules
pentest_allowed_hosts: []   # empty = Step 4 forbidden
accepted_risks: []          # requires User approve via Orchestrator (decisions.md IDs)
waive_buckets: []           # empty by default — cannot waive injection/auth without PO
```

`asvs_level` controls audit **depth per bucket**, not the bucket list — Step 3 buckets are
fixed at L0. Raising the level adds checks (see mapping table in the L1 template); lowering
it below the `project_profile` default requires an `accepted_risks` entry.
