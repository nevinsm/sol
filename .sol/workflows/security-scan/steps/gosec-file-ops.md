# SAST: File Operation Risks

Run gosec to detect file operation vulnerabilities, then validate each finding against the actual code.

## Tool Execution

Run gosec filtered to file-operation rules:

```bash
gosec -include=G301,G302,G303,G304,G305,G306,G307 -fmt=json -quiet ./...
```

**Rule coverage:**
- **G301**: Poor file permissions used with os.Mkdir
- **G302**: Poor file permissions used with os.MkdirAll
- **G303**: Creating tempfiles with predictable names (os.Create on predictable path)
- **G304**: File path provided as taint input (path traversal)
- **G305**: Zip/tar slip — file extraction without path validation
- **G306**: Poor file permissions used with os.WriteFile / ioutil.WriteFile
- **G307**: Deferred file close on writable file (errors not checked)

If gosec is not installed, install it:
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

## Validation

For each gosec finding:

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

Disposition each finding:
- **Confirmed** — real file operation risk with exploitable or data-integrity impact
- **Confirmed (reduced)** — real issue but mitigated by context (e.g., single-user CLI tool)
- **False positive** — file path is not attacker-controlled, or permissions are appropriate for context
- **Needs investigation** — cannot determine exploitability from static analysis

## Beyond gosec

After processing gosec output, manually check:

- **Symlink following** — does the code follow symlinks without checking? An attacker could symlink a writable path to a sensitive file
- **Race conditions (TOCTOU)** — check-then-act patterns on file existence/permissions (e.g., `os.Stat` followed by `os.Open`)
- **World-writable directories** — writing sensitive data to /tmp or other shared directories without restrictive permissions
- **Git worktree paths** — since this codebase manages git worktrees, check that worktree path construction cannot escape the intended directory

## Output

Write all findings to `review.md` in your writ output directory.

Each finding must include:
1. One-line summary
2. gosec rule ID or "MANUAL"
3. File path and line range
4. **The actual code** — quote the specific lines
5. Path/permission analysis — where does the path come from? What are the effective permissions?
6. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
7. CWE ID (e.g., CWE-22 for path traversal, CWE-732 for incorrect permissions, CWE-377 for insecure temp file, CWE-61 for symlink following)

## Severity Guide

- **CRITICAL**: Path traversal allowing read/write to arbitrary files via attacker-controlled input
- **HIGH**: World-writable sensitive files (credentials, database), symlink attacks on trusted paths
- **MEDIUM**: Overly permissive file/directory creation, TOCTOU races on security-relevant paths
- **LOW**: Deferred close without error check, slightly loose permissions on non-sensitive files

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Include the code.** Every finding must quote the specific lines from the source.

**Consider the deployment model.** This is a CLI tool and local daemon running as a single user. File permission findings should be assessed in that context — overly permissive files matter less on a single-user system than on a shared server.
