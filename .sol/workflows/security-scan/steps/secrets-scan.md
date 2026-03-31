# Secrets Detection Scan

Scan the codebase for hardcoded secrets, credentials, API keys, and sensitive tokens.

## Approach

This step does NOT rely on a single tool — use a combination of pattern matching and manual review to find secrets that should not be in source control.

### Step 1: Pattern-Based Search

Search the codebase for common secret patterns. Run these searches and review the results:

```bash
# High-entropy strings that look like API keys/tokens (base64, hex)
grep -rn --include='*.go' -E '("|`)[A-Za-z0-9+/=]{32,}("|`)' .
grep -rn --include='*.go' -E '("|`)[0-9a-f]{32,}("|`)' .

# Explicit secret/key/token/password variable names with string assignments
grep -rn --include='*.go' -iE '(secret|apikey|api_key|token|password|passwd|credential|private.?key)\s*[:=]' .

# AWS-style keys
grep -rn --include='*.go' -E 'AKIA[0-9A-Z]{16}' .

# Connection strings with embedded credentials
grep -rn --include='*.go' -iE '(mysql|postgres|mongodb|redis)://[^/]*:[^@]*@' .

# Private key markers
grep -rn -E 'BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY' .
```

Also check non-Go files that might contain secrets:
```bash
# Config/env files
find . -name '*.env' -o -name '*.env.*' -o -name '.env*' | head -20
find . -name '*.pem' -o -name '*.key' -o -name '*.p12' -o -name '*.pfx' | head -20

# TOML/YAML/JSON config files with potential secrets
grep -rn --include='*.toml' --include='*.yaml' --include='*.yml' --include='*.json' -iE '(secret|key|token|password|credential)' .
```

### Step 2: Baseline Pre-Filter

Before validating findings, read the baseline file at:
`.sol/workflows/security-scan/baseline.json`

For each finding from Step 1, check if it matches a baseline entry:
- Compare file path (or `*` for wildcard entries), rule ID, and CWE
- **If matched with category `false_positive`**: skip entirely — do not include in review.md
- **If matched with category `accepted`**: skip entirely — do not include in review.md
- **If matched with category `deferred`**: include in review.md with a note that it was previously deferred

At the end of review.md, include a brief "Baseline Filtering" summary:
- How many findings were filtered by baseline
- Any baseline entries that no longer match any finding (may indicate stale entries)

### Step 3: Context Validation

For each match from Step 1:

1. **Read the surrounding code** — is this a real secret or a variable name, struct field, config key, or test fixture?
2. **Check for environment variable indirection** — `os.Getenv("SECRET")` is fine; `secret := "hardcoded-value"` is not
3. **Check for test fixtures** — test files may contain fake credentials. These are lower severity but should still be flagged if they look like real credentials.
4. **Check git history** — if a file previously contained secrets that were "removed", they're still in history. Note this but don't deep-dive git history.

### Step 4: Structural Review

Beyond pattern matching, review these high-risk areas:

- **Configuration loading** (`internal/config/`): how are secrets expected to be provided? Is there a path where defaults could expose a secret?
- **Environment file handling** (`internal/envfile/`): does the env file parser handle secret values safely? Could env files with secrets be logged or exposed?
- **Session command construction** (`internal/session/`): are credentials or tokens passed via command-line arguments (visible in `ps`)?
- **Git operations** (`internal/git/`): are any credentials embedded in git URLs or passed as arguments?
- **HTTP/API clients**: any hardcoded bearer tokens, basic auth credentials, or API keys?

### Step 5: .gitignore Review

Check that sensitive file patterns are properly excluded from version control:

```bash
cat .gitignore
```

Verify that patterns exist for: `*.env`, `*.pem`, `*.key`, credentials files, local config overrides with secrets.

## Output

Write all findings to `review.md` in your writ output directory.

### Findings (for triage)

Only findings the agent believes are confirmed or ambiguous. These go to triage for validation.

Each finding must include:
1. One-line summary
2. Detection method: "PATTERN" (regex match), "MANUAL" (structural review), or "GITIGNORE" (missing exclusion)
3. File path and line range
4. **The actual code** — quote the specific lines (but redact any actual secret values to their first 4 characters + "...")
5. Assessment — is this a real secret, a placeholder, a test fixture, or a false positive?
6. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
7. CWE ID (CWE-798 for hardcoded credentials, CWE-312 for cleartext storage, CWE-522 for insufficiently protected credentials)

### Filtered (appendix)

Findings confidently determined to be false positives. Brief one-line entries with detection method, file, and reason. Triage may spot-check but doesn't need to re-validate each one.

### Baseline Filtering

Summary of baseline pre-filter results (count filtered, stale entries).

## Severity Guide

- **CRITICAL**: Real production credentials, API keys, or private keys in source code
- **HIGH**: Credentials in config files that could be committed, missing .gitignore patterns for secret files
- **MEDIUM**: Test fixtures with realistic-looking credentials, secrets passed via CLI arguments (visible in process list)
- **LOW**: Placeholder/example values that could be mistaken for real secrets, minor .gitignore gaps

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Redact actual secrets.** If you find a real secret, quote only enough to identify it (first 4 chars). Do not reproduce full secrets in your review.

**False positives are expected.** Pattern-based secret detection has a high false-positive rate. Be thorough in your validation — the triage step depends on accurate dispositions.
