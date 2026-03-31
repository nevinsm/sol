# SAST: Injection and File Operation Risks

Analyze gosec findings related to injection vulnerabilities and file operation risks. This step reads the shared `gosec-raw.json` from the `gosec-run` step — do NOT run gosec yourself.

## Load Findings

Read `gosec-raw.json` from the `gosec-run` step's output directory.

Filter for these rules:
- **G201**: SQL query construction using format string
- **G202**: SQL query construction using string concatenation
- **G203**: SQL query construction using substrings/joins
- **G204**: Subprocess launched with variable (command injection)
- **G301**: Poor file permissions used with os.Mkdir
- **G302**: Poor file permissions used with os.MkdirAll
- **G304**: File path provided as taint input (path traversal)
- **G305**: Zip/tar slip — file extraction without path validation
- **G306**: Poor file permissions used with os.WriteFile / ioutil.WriteFile
- **G307**: Deferred file close on writable file (errors not checked)

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

## Validation — Injection Findings (G201-G204)

For each injection finding:

1. **Read the cited file and line** — confirm the code exists as reported
2. **Trace the input** — is the flagged variable actually derived from external input (user input, environment, file content, network)? Or is it a hardcoded/internal value?
3. **Check for existing sanitization** — is the input validated, escaped, or parameterized before reaching the sink?
4. **Assess exploitability** — given the application context (CLI tool, local daemon), can this actually be exploited? A format-string SQL query fed only by internal constants is not exploitable.

## Validation — File Operation Findings (G301, G302, G304-G307)

For each file operation finding:

1. **Read the cited file and line** — confirm the code exists as reported
2. **Trace the file path source**:
   - Is the path derived from user input, environment variables, or configuration files?
   - Is the path constructed from internal constants or trusted sources?
   - For path traversal (G304): can an attacker influence the path components?
3. **Check permissions context**:
   - For permission findings (G301/G302/G306): what data is being written? Config files, logs, SQLite databases, temporary files?
   - Are the permissions appropriate for the file's sensitivity and multi-user context?
   - Does the application run as root or a shared user?
4. **Assess archive extraction**:
   - For G305: does the codebase extract archives? If so, is path validation performed?
5. **Check deferred close patterns**:
   - For G307: is the file opened for writing? Is the deferred Close result actually important for data integrity?

## Disposition

Disposition each finding:
- **Confirmed** — tainted input reaches a dangerous sink without sanitization, or real file operation risk with exploitable impact
- **Confirmed (reduced)** — real issue but lower severity than gosec reports (e.g., input is semi-trusted, single-user CLI context)
- **False positive** — input is not attacker-controlled, or adequate sanitization exists, or permissions are appropriate
- **Needs investigation** — cannot determine exploitability from static analysis alone

## Beyond gosec

After processing gosec output, manually check for patterns gosec may miss:

### Injection Patterns
- **Template injection**: `text/template` or `html/template` with user-controlled template strings (not just data)
- **LDAP/header injection**: if the codebase interacts with LDAP or constructs HTTP headers from untrusted input
- **Log injection**: untrusted data written to structured logs without sanitization (can corrupt log parsing)

### File Operation Patterns
- **Symlink following** — does the code follow symlinks without checking? An attacker could symlink a writable path to a sensitive file
- **Race conditions (TOCTOU)** — check-then-act patterns on file existence/permissions (e.g., `os.Stat` followed by `os.Open`)
- **World-writable directories** — writing sensitive data to /tmp or other shared directories without restrictive permissions
- **Git worktree paths** — since this codebase manages git worktrees, check that worktree path construction cannot escape the intended directory

## Output

Write all findings to `review.md` in your writ output directory.

### Findings (for triage)

Only findings the agent believes are confirmed or ambiguous. These go to triage for validation.

Each finding must include:
1. One-line summary
2. gosec rule ID (if tool-detected) or "MANUAL" (if manually found)
3. File path and line range
4. **The actual code** — quote the specific lines
5. Input/path trace — where does the tainted data or file path originate?
6. Exploitability assessment — concrete attack scenario or explanation of why it's not exploitable
7. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
8. CWE ID (e.g., CWE-89 for SQL injection, CWE-78 for OS command injection, CWE-22 for path traversal, CWE-732 for incorrect permissions)

### Filtered (appendix)

Findings confidently determined to be false positives. Brief one-line entries with rule ID, file, and reason. Triage may spot-check but doesn't need to re-validate each one.

### Baseline Filtering

Summary of baseline pre-filter results (count filtered, stale entries).

## Severity Guide

### Injection
- **CRITICAL**: Attacker-controlled input reaches SQL/command execution with no sanitization
- **HIGH**: Semi-trusted input reaches dangerous sink; exploitation requires specific conditions
- **MEDIUM**: Internal input reaches dangerous sink; defense-in-depth concern
- **LOW**: Pattern matches but no realistic attack vector in this application context

### File Operations
- **CRITICAL**: Path traversal allowing read/write to arbitrary files via attacker-controlled input
- **HIGH**: World-writable sensitive files (credentials, database), symlink attacks on trusted paths
- **MEDIUM**: Overly permissive file/directory creation, TOCTOU races on security-relevant paths
- **LOW**: Deferred close without error check, slightly loose permissions on non-sensitive files

## Constraints

**DO NOT run gosec.** Read from the shared `gosec-raw.json` output.

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Include the code.** Every finding must quote the specific lines from the source.

**Trace inputs.** A gosec finding without input-origin analysis is incomplete. Always determine where the flagged variable comes from.

**Be honest about false positives.** gosec is noisy on some rules (G204, G304, G301 especially). If a finding is a false positive, say so clearly — classify it in the Filtered appendix.

**Consider the deployment model.** This is a CLI tool and local daemon running as a single user. File permission findings should be assessed in that context.
