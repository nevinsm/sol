# SAST: Error Handling, Concurrency, and Cryptography

Analyze gosec findings related to error handling gaps, concurrency issues, and cryptographic weaknesses. This step reads the shared `gosec-raw.json` from the `gosec-run` step — do NOT run gosec yourself.

## Load Findings

Read `gosec-raw.json` from the `gosec-run` step's output directory.

Filter for these rules:
- **G104**: Audit errors not checked
- **G401**: Use of DES/RC4 or other weak cipher
- **G402**: TLS with InsecureSkipVerify
- **G403**: Use of weak RSA key (< 2048 bits)
- **G404**: Use of weak random number generator (math/rand instead of crypto/rand)
- **G405**: Use of deprecated DES/3DES
- **G501**: Import of deprecated crypto/md5
- **G502**: Import of deprecated crypto/des
- **G503**: Import of deprecated crypto/rc4
- **G504**: Import of deprecated net/http/cgi
- **G505**: Import of deprecated crypto/sha1
- **G506**: Use of ssh.InsecureIgnoreHostKey
- **G601**: Implicit memory aliasing of items from a range statement (can cause data races)

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

## Validation — Error Handling (G104)

**Important**: G104 is the noisiest gosec rule. Most findings will be false positives or low-severity. Your primary job is to filter for the security-relevant subset.

### Report (security-relevant error suppression)

- **Authentication/authorization paths**: errors in credential validation, permission checks, token verification
- **Cryptographic operations**: errors from encryption, hashing, signature verification
- **File operations on sensitive data**: errors writing credentials, database files, tether state
- **Network/TLS operations**: errors in connection setup, certificate validation
- **Database write operations**: errors on INSERT/UPDATE/DELETE that affect system state
- **Process execution**: errors from exec.Command that could indicate injection success/failure
- **Input validation**: errors from parsing/validating untrusted input

### Skip (benign error suppression)

- **Deferred Close() on read-only files** — closing a reader rarely fails meaningfully
- **fmt.Fprintf to stdout/stderr** — write errors to terminal are not actionable
- **Logger calls** — logging frameworks handle their own error paths
- **String conversion utilities** — strconv on known-format internal strings
- **Buffer writes** — bytes.Buffer.Write never returns an error

For each security-relevant finding:

1. **Read the cited file and line** — confirm the code exists
2. **Determine what operation's error is being discarded** — what function returns the unchecked error?
3. **Assess the security impact** — if this error occurred silently, what would happen? Would the system continue with invalid state? Would a security check be bypassed?
4. **Check for compensating controls** — is the error handled elsewhere? Is there a retry? Does the caller check a different signal?

## Validation — Concurrency (G601)

1. **Check Go version** — G601 was fixed in Go 1.22 (loop variable semantics changed). If the project uses Go 1.22+, most G601 findings are false positives. Check `go.mod` for the Go version.
2. **If pre-1.22 or the aliased pointer escapes**: determine if the aliased value is used concurrently or stored for later use
3. **Assess security impact** — does the race condition affect authentication, authorization, state integrity, or process control?

## Validation — Cryptographic Issues (G401-G506)

For each crypto finding:

1. **Read the cited file and line** — confirm the code exists as reported
2. **Determine the purpose** — what is the crypto being used for? Authentication, integrity checks, unique IDs, test fixtures?
3. **Assess the context**:
   - `math/rand` for generating non-security-sensitive IDs (e.g., writ IDs from hex encoding) is different from `math/rand` for generating auth tokens
   - `InsecureSkipVerify` in test code or with a documented reason (self-signed certs in local dev) is different from production TLS
   - MD5/SHA1 used for non-security checksums (cache keys, dedup) vs. for authentication
4. **Check for crypto/rand alternatives** — if `math/rand` is used, is `crypto/rand` used elsewhere in the codebase for the same purpose?

## Disposition

Disposition each finding:
- **Confirmed** — real security issue with impact
- **Confirmed (reduced)** — real issue but context limits severity
- **False positive** — not security-relevant given the context
- **Needs investigation** — cannot determine from static analysis alone

## Beyond gosec

After processing gosec output, manually check for patterns gosec may miss:

### Error Handling Patterns
- **Error-as-success confusion**: checking `err == nil` when the function signals failure via a different return value
- **Partial error handling**: checking the error but not acting on it (logging then continuing with nil/zero value)
- **Panic recovery swallowing security errors**: `recover()` catching panics from security-critical code paths
- **Context cancellation masking errors**: operations that fail due to canceled context where the cancellation hides a security-relevant error

### Concurrency Patterns
- **TOCTOU races on security-sensitive state**: check-then-act patterns where security state can change between check and act
- **Lock contract analysis**: review the codebase for shared mutable state in:
  - Tether management (`internal/tether/`): concurrent tether creation/deletion, file-based state races
  - Session lifecycle (`internal/session/`, `internal/dispatch/`): concurrent session start/stop, state transitions
  - Store access (`internal/store/`): concurrent SQLite operations, transaction isolation
  - Forge pipeline (`internal/forge/`): concurrent merge operations, state machine transitions
  - Service management (`internal/service/`): concurrent service start/stop, health check races
- **Map access without synchronization**: Go maps are not goroutine-safe
- **Goroutine leaks in security paths**: goroutines spawned for security operations that are never joined
- **Signal handling races**: concurrent signal handlers modifying shared state during graceful shutdown

### Cryptographic Patterns
- **Hardcoded cryptographic keys or salts** — any `[]byte` literals that look like keys or initialization vectors
- **Predictable seeds** — `rand.Seed(time.Now().UnixNano())` or similar predictable seeding
- **Missing key rotation** — long-lived keys without rotation mechanism
- **Timing side channels** — string comparison of secrets using `==` instead of `subtle.ConstantTimeCompare`

## Output

Write all findings to `review.md` in your writ output directory.

### Findings (for triage)

Only findings the agent believes are confirmed or ambiguous. These go to triage for validation.

Each finding must include:
1. One-line summary
2. gosec rule ID, "MANUAL", or "RACE-DETECTOR"
3. File path and line range
4. **The actual code** — quote the specific lines
5. Security impact assessment — what happens if this issue is exploited or the error occurs silently?
6. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
7. CWE ID (e.g., CWE-391 for unchecked error, CWE-362 for race condition, CWE-327 for broken crypto, CWE-330 for insufficient randomness)

### Filtered (appendix)

Findings confidently determined to be false positives. Brief one-line entries with rule ID, file, and reason. Triage may spot-check but doesn't need to re-validate each one.

### Baseline Filtering

Summary of baseline pre-filter results (count filtered, stale entries).

## Severity Guide

### Error Handling
- **CRITICAL**: Unchecked error bypasses authentication or authorization
- **HIGH**: Unchecked error on security-critical write (credential storage, state file that gates access)
- **MEDIUM**: Unchecked error on database write or process execution in sensitive paths
- **LOW**: Unchecked error on read operations in security-adjacent code

### Concurrency
- **CRITICAL**: Race condition that can bypass authentication/authorization or corrupt security state
- **HIGH**: Race condition on state that gates process execution or data integrity
- **MEDIUM**: Race condition on operational state (health checks, status, metrics) or with narrow window
- **LOW**: Theoretical race with no practical security impact, or G601 in Go 1.22+

### Cryptography
- **CRITICAL**: Weak crypto protecting authentication, authorization, or sensitive data
- **HIGH**: Predictable randomness in security-sensitive contexts (token generation, session IDs)
- **MEDIUM**: Weak crypto in non-critical contexts; `math/rand` where `crypto/rand` would be more appropriate
- **LOW**: Deprecated crypto imports in test code or non-security contexts

## Constraints

**DO NOT run gosec.** Read from the shared `gosec-raw.json` output.

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Include the code.** Every finding must quote the specific lines from the source.

**Be ruthless about filtering G104.** Your value is the security filter, not a raw dump. If a finding is not security-relevant, classify it in the Filtered appendix.

**Context matters for crypto.** `math/rand` for generating hex suffixes on local file names is not the same severity as `math/rand` for generating API tokens.

**Practical severity for concurrency.** A race condition in a test helper is not the same as a race in the forge merge pipeline. Assess based on real-world impact.
