# SAST: Cryptographic Issues

Run gosec to detect cryptographic weaknesses, then validate each finding against the actual code.

## Tool Execution

Run gosec filtered to crypto-related rules:

```bash
gosec -include=G401,G402,G403,G404,G405,G501,G502,G503,G504,G505,G506 -fmt=json -quiet ./...
```

**Rule coverage:**
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

If gosec is not installed, install it:
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

## Validation

For each gosec finding:

1. **Read the cited file and line** — confirm the code exists as reported
2. **Determine the purpose** — what is the crypto being used for? Authentication, integrity checks, unique IDs, test fixtures?
3. **Assess the context**:
   - `math/rand` for generating non-security-sensitive IDs (e.g., writ IDs from hex encoding) is different from `math/rand` for generating auth tokens
   - `InsecureSkipVerify` in test code or with a documented reason (self-signed certs in local dev) is different from production TLS
   - MD5/SHA1 used for non-security checksums (cache keys, dedup) vs. for authentication
4. **Check for crypto/rand alternatives** — if `math/rand` is used, is `crypto/rand` used elsewhere in the codebase for the same purpose?

Disposition each finding:
- **Confirmed** — weak crypto used in a security-sensitive context
- **Confirmed (reduced)** — real issue but context limits severity (e.g., math/rand for non-sensitive IDs)
- **False positive** — crypto usage is appropriate for the context (non-security purpose, test code)
- **Needs investigation** — cannot determine whether the context is security-sensitive

## Beyond gosec

After processing gosec output, manually check:

- **Hardcoded cryptographic keys or salts** — any `[]byte` literals that look like keys or initialization vectors
- **Predictable seeds** — `rand.Seed(time.Now().UnixNano())` or similar predictable seeding
- **Missing key rotation** — long-lived keys without rotation mechanism
- **Timing side channels** — string comparison of secrets using `==` instead of `subtle.ConstantTimeCompare`

## Output

Write all findings to `review.md` in your writ output directory.

Each finding must include:
1. One-line summary
2. gosec rule ID or "MANUAL"
3. File path and line range
4. **The actual code** — quote the specific lines
5. Context assessment — what is the crypto being used for?
6. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
7. CWE ID (e.g., CWE-327 for broken crypto, CWE-330 for insufficient randomness, CWE-326 for inadequate key length)

## Severity Guide

- **CRITICAL**: Weak crypto protecting authentication, authorization, or sensitive data
- **HIGH**: Predictable randomness in security-sensitive contexts (token generation, session IDs)
- **MEDIUM**: Weak crypto in non-critical contexts; `math/rand` where `crypto/rand` would be more appropriate
- **LOW**: Deprecated crypto imports in test code or non-security contexts

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Include the code.** Every finding must quote the specific lines from the source.

**Context matters.** `math/rand` for generating hex suffixes on local file names is not the same severity as `math/rand` for generating API tokens. Always assess the security relevance of the context.
