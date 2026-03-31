# Security Review

Review the code changes for security vulnerabilities.

Examine the branch diff against main. Think like an attacker.

**Look for:**
- Input validation gaps — user-supplied data used without sanitization
- Authentication/authorization bypasses — missing checks on endpoints
- Injection vulnerabilities — SQL, command injection, template injection
- Sensitive data exposure — secrets in logs, error messages, or responses
- Hardcoded secrets or credentials — API keys, passwords, tokens in source
- Path traversal vulnerabilities — user input used in file paths without cleaning
- SSRF (Server-Side Request Forgery) — user-controlled URLs fetched server-side
- Insecure cryptographic usage — weak algorithms, poor random sources
- OWASP Top 10 concerns relevant to the change
- Improper error handling that leaks internal details to callers
- Missing TLS/encryption for sensitive data in transit or at rest
- File permissions that are too broad

**Questions to answer:**
- What can a malicious user do with this code?
- What data could be exposed if this fails?
- Are there defense-in-depth gaps?
- Are secrets properly managed (env vars, config) not hardcoded?
- Is user input treated as untrusted at all boundaries?

Flag any P0 security issue prominently — these block merge.
