# Authentication & Authorization Review

Review authentication and authorization mechanisms for weaknesses.

Scope: {{scope}}
{{#focus}}Focus: {{focus}}{{/focus}}

Examine:
- Trust boundaries: where authentication is checked and where it is assumed
- Privilege escalation paths: horizontal and vertical
- Session management: creation, validation, expiration, revocation
- Credential storage: hashing algorithms, salting, key derivation
- Access control consistency: are checks applied uniformly across all entry points
- Token handling: JWT validation, refresh token rotation, scope enforcement
- Role and permission models: granularity, default permissions, least privilege
