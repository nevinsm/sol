# SAST: Security-Relevant Error Handling

Run gosec to detect unhandled errors that could mask security failures, then validate each finding against the actual code.

## Tool Execution

Run gosec filtered to error-handling rules:

```bash
gosec -include=G104 -fmt=json -quiet ./...
```

**Rule coverage:**
- **G104**: Audit errors not checked — function return values that include an error are not being checked

**Important**: G104 is the noisiest gosec rule. Most findings will be false positives or low-severity. Your primary job is to filter for the security-relevant subset.

If gosec is not installed, install it:
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

## Validation — Security Filter

G104 will produce many findings. Apply this filter to separate security-relevant from benign:

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

## Beyond gosec

After processing gosec output, manually check for patterns gosec misses:

- **Error-as-success confusion**: checking `err == nil` when the function signals failure via a different return value
- **Partial error handling**: checking the error but not acting on it (logging then continuing with nil/zero value)
- **Panic recovery swallowing security errors**: `recover()` catching panics from security-critical code paths
- **Context cancellation masking errors**: operations that fail due to canceled context where the cancellation hides a security-relevant error

## Output

Write all findings to `review.md` in your writ output directory.

**Only report security-relevant error handling issues.** A review.md with 200 "unchecked Close()" findings is useless. Focus on the findings that could lead to security bypass, data corruption, or silent failures in security-critical paths.

Each finding must include:
1. One-line summary
2. gosec rule ID or "MANUAL"
3. File path and line range
4. **The actual code** — quote the specific lines
5. Security impact — what happens if this error occurs silently?
6. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
7. CWE ID (CWE-391 for unchecked error condition, CWE-754 for improper check for unusual conditions)

## Severity Guide

- **CRITICAL**: Unchecked error bypasses authentication or authorization
- **HIGH**: Unchecked error on security-critical write (credential storage, state file that gates access)
- **MEDIUM**: Unchecked error on database write or process execution in sensitive paths
- **LOW**: Unchecked error on read operations in security-adjacent code

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Be ruthless about filtering.** G104 is noisy. Your value is the security filter, not a raw dump of gosec output. If a finding is not security-relevant, do not include it.
