# OWASP Top 10 Review

Review for OWASP Top 10 vulnerability categories.

Scope: {{scope}}
{{#focus}}Focus: {{focus}}{{/focus}}

Examine each category:
- A01 Broken Access Control: missing authorization checks, IDOR, privilege escalation
- A02 Cryptographic Failures: weak algorithms, plaintext storage, missing encryption
- A03 Injection: SQL, NoSQL, OS command, LDAP injection vectors
- A04 Insecure Design: missing threat modeling, insecure business logic
- A05 Security Misconfiguration: default credentials, unnecessary features, verbose errors
- A06 Vulnerable and Outdated Components: known CVEs, unsupported frameworks
- A07 Identification and Authentication Failures: weak passwords, missing MFA, session fixation
- A08 Software and Data Integrity Failures: insecure deserialization, unsigned updates
- A09 Security Logging and Monitoring Failures: missing audit trails, unmonitored events
- A10 Server-Side Request Forgery: unvalidated URLs, internal service access
