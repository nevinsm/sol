# Dependency Vulnerability Audit

Run govulncheck to identify known vulnerabilities in project dependencies, then assess impact.

## Tool Execution

Run govulncheck against the full module:

```bash
govulncheck ./...
```

This analyzes the project's dependency graph and reports CVEs that affect code paths actually used by the project (not just present in the dependency tree).

If govulncheck is not installed, install it:
```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

Also run in binary analysis mode if a binary exists:
```bash
if [ -f bin/sol ]; then
  govulncheck -mode=binary bin/sol
fi
```

### Supplementary: go.mod Review

Review `go.mod` for:
- **Pinned old versions** — dependencies that haven't been updated in a long time
- **Replace directives** — local replacements that might bypass security fixes
- **Indirect dependencies** — transitive deps that are known-vulnerable

```bash
# Check for outdated direct dependencies
go list -m -u all 2>/dev/null | grep '\[' | head -30
```

## Baseline Pre-Filter

Before validating findings, read the baseline file at:
`.sol/workflows/security-scan/baseline.json`

For each finding, check if it matches a baseline entry:
- Compare file path (or `*` for wildcard entries), rule ID, and CWE
- **If matched with category `false_positive`**: skip entirely — do not include in review.md
- **If matched with category `accepted`**: skip entirely — do not include in review.md
- **If matched with category `deferred`**: include in review.md with a note that it was previously deferred

At the end of review.md, include a brief "Baseline Filtering" summary:
- How many findings were filtered by baseline
- Any baseline entries that no longer match any finding (may indicate stale entries)

## Validation

For each govulncheck finding:

1. **Read the CVE details** — what is the vulnerability? What versions are affected?
2. **Trace the call path** — govulncheck shows which function in your code calls the vulnerable function. Read that code path.
3. **Assess exploitability**:
   - Does the vulnerable code path handle attacker-controlled input?
   - Is the vulnerable function called with parameters that trigger the vulnerability?
   - Are there mitigating factors (input validation, restricted network access, sandboxing)?
4. **Check fix availability** — is there a patched version of the dependency? Is upgrading feasible (breaking changes)?

Disposition each finding:
- **Confirmed** — vulnerable code path is reachable and exploitable
- **Confirmed (reduced)** — vulnerability exists but exploitation requires conditions unlikely in this application
- **Not exploitable** — the call path exists but parameters/context prevent exploitation
- **Fixed available** — a simple version bump resolves the issue

## Beyond govulncheck

govulncheck only covers the Go vulnerability database. Additionally check:

- **License compliance** — are there dependencies with restrictive licenses (GPL, AGPL) that conflict with the project's licensing?
- **Unmaintained dependencies** — key dependencies with no commits in 2+ years and known open issues
- **Dependency confusion risk** — any internal package names that could be shadowed by public modules?

## Output

Write all findings to `review.md` in your writ output directory.

### Findings (for triage)

Only findings the agent believes are confirmed or ambiguous. These go to triage for validation.

Each finding must include:
1. One-line summary
2. CVE ID (or "MANUAL" for non-CVE findings)
3. Affected dependency and version
4. **The call path** — quote the govulncheck output showing how your code reaches the vulnerable function
5. Exploitability assessment — can this be triggered in the context of this application?
6. Fix approach — version bump, dependency replacement, or code change
7. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
8. CWE ID where applicable

### Filtered (appendix)

Findings confidently determined to be false positives or not exploitable. Brief one-line entries with CVE ID, dependency, and reason. Triage may spot-check but doesn't need to re-validate each one.

### Baseline Filtering

Summary of baseline pre-filter results (count filtered, stale entries).

## Severity Guide

- **CRITICAL**: Exploitable RCE or auth bypass in a reachable code path
- **HIGH**: Exploitable vulnerability in a reachable code path (DoS, info disclosure, privilege escalation)
- **MEDIUM**: Vulnerability in reachable code but exploitation requires specific conditions unlikely in this context
- **LOW**: Vulnerability in dependency but call path does not reach the vulnerable function, or fix is a simple version bump with no exploitation risk

## Constraints

**DO NOT modify any source code or go.mod.** This is a read-only analysis. Your only deliverable is `review.md`.

**Include the call path.** govulncheck's value is that it shows reachability. Always include the call chain in your findings.

**Assess, don't just list.** A raw dump of CVEs is not useful. For every finding, explain whether it's exploitable in this specific application context.
