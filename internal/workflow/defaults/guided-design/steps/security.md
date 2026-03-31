# Security Exploration

## Design Task

{{target.description}}

---

Explore security implications: authentication, authorization, input validation, secrets handling, and attack surface.

Focus: What are the trust boundaries and what could go wrong?

**Explore:**
- Trust boundaries: what trusts what? Where are the privilege transitions?
- Credential storage: how are secrets, tokens, and API keys stored and accessed?
- Input validation: where is untrusted input accepted? What sanitization is needed?
- Audit logging: what security-relevant events should be logged?
- Attack surface: what new inputs, outputs, or permissions does this introduce?
- Threat model: who might misuse this and how? Malicious agents, compromised sessions?
- File system security: permissions on created files, symlink attacks, path traversal
- Process isolation: what runs with elevated privileges? Can it be reduced?
- Secrets in logs: could sensitive data leak into logs, error messages, or status output?
- Dependency risk: do new dependencies introduce supply chain risk?
- Failure modes: what happens when a security check fails — fail open or fail closed?
- Defense in depth: what layered protections exist beyond the primary control?

**Questions to answer:**
- What is the worst-case outcome if this feature is exploited or misused?
- What new permissions, file access, or network access does this require?
- How are inputs validated and sanitized before use?
- Are there defense-in-depth opportunities beyond the primary security mechanism?

**Output format:**
```
## Summary
(1-2 paragraphs: security posture and key concerns)

## Key Decisions Identified
For each decision point:
### Decision: <title>
- **Options**: <list the viable approaches>
- **Tradeoffs**: <what you gain/lose with each>
- **Recommendation**: <preferred option and why>

## Risks and Concerns
- ...

## Recommendations
- ...
```
