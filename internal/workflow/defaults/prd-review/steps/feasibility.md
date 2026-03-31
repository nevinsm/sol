# Technical Feasibility

Assess technical feasibility and identify hard engineering challenges.

Look for:
- Features that assume capabilities the system doesn't have
- Third-party dependencies that may not support this use case
- Performance requirements that may be fundamentally hard to meet
- Implicit coupling: does this require changing things the PRD doesn't mention?
- Missing prerequisite work: what has to be built first?
- Privacy or security requirements with significant implementation cost
- Scale requirements that would require substantial infrastructure work

Questions to answer:
- What's the hardest technical problem in this PRD?
- Are there requirements that are technically impossible or very expensive?
- What unstated technical constraints or prerequisites exist?
- What would double the implementation effort if discovered mid-build?
