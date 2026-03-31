# SAST: Injection Vulnerabilities

Run gosec to detect injection vulnerabilities, then validate each finding against the actual code.

## Tool Execution

Run gosec filtered to injection-related rules:

```bash
gosec -include=G201,G202,G203,G204,G304 -fmt=json -quiet ./...
```

**Rule coverage:**
- **G201**: SQL query construction using format string
- **G202**: SQL query construction using string concatenation
- **G203**: SQL query construction using substrings/joins
- **G204**: Subprocess launched with variable (command injection)
- **G304**: File path provided as taint input (path injection)

If gosec is not installed, install it:
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

## Validation

For each gosec finding:

1. **Read the cited file and line** — confirm the code exists as reported
2. **Trace the input** — is the flagged variable actually derived from external input (user input, environment, file content, network)? Or is it a hardcoded/internal value?
3. **Check for existing sanitization** — is the input validated, escaped, or parameterized before reaching the sink?
4. **Assess exploitability** — given the application context (CLI tool, local daemon), can this actually be exploited? A format-string SQL query fed only by internal constants is not exploitable.

Disposition each finding:
- **Confirmed** — tainted input reaches a dangerous sink without sanitization
- **Confirmed (reduced)** — real issue but lower severity than gosec reports (e.g., input is semi-trusted)
- **False positive** — input is not attacker-controlled, or adequate sanitization exists
- **Needs investigation** — cannot determine exploitability from static analysis alone

## Beyond gosec

After processing gosec output, manually check for injection patterns gosec may miss:

- **Template injection**: `text/template` or `html/template` with user-controlled template strings (not just data)
- **LDAP/header injection**: if the codebase interacts with LDAP or constructs HTTP headers from untrusted input
- **Log injection**: untrusted data written to structured logs without sanitization (can corrupt log parsing)

## Output

Write all findings to `review.md` in your writ output directory.

Each finding must include:
1. One-line summary
2. gosec rule ID (if tool-detected) or "MANUAL" (if manually found)
3. File path and line range
4. **The actual code** — quote the specific lines
5. Input trace — where does the tainted data originate?
6. Exploitability assessment — concrete attack scenario or explanation of why it's not exploitable
7. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
8. CWE ID (e.g., CWE-89 for SQL injection, CWE-78 for OS command injection, CWE-22 for path traversal)

## Severity Guide

- **CRITICAL**: Attacker-controlled input reaches SQL/command execution with no sanitization
- **HIGH**: Semi-trusted input reaches dangerous sink; exploitation requires specific conditions
- **MEDIUM**: Internal input reaches dangerous sink; defense-in-depth concern
- **LOW**: Pattern matches but no realistic attack vector in this application context

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Include the code.** Every finding must quote the specific lines from the source.

**Trace inputs.** A gosec finding without input-origin analysis is incomplete. Always determine where the flagged variable comes from.

**Be honest about false positives.** gosec is noisy on some rules. If a finding is a false positive, say so clearly — this feeds into baseline decisions downstream.
