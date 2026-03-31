# Missing Requirements

Identify requirements that are completely absent from the PRD.

Look for:
- Authentication and authorization: who can do this?
- Data migration: what happens to existing data?
- Backwards compatibility: does this break anything existing?
- Edge cases with empty, null, or zero states
- Concurrent access: what if two users do this simultaneously?
- Admin tooling: how will support teams debug issues?
- Deprecation and cleanup: how does old behavior get removed?
- Rate limiting and abuse prevention
- Audit logging and compliance requirements

Questions to answer:
- What completely unaddressed scenarios could cause a production incident?
- What will the next engineer touching this code wish had been specified?
- What will ops ask about at launch that nobody thought about?
