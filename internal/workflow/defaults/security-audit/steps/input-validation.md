# Input Validation Review

Review input handling and validation at system boundaries.

Scope: {{scope}}
{{focus}}

Examine:
- Validation at system boundaries: API endpoints, file uploads, user input, IPC
- Output encoding: HTML, URL, JavaScript, SQL context-appropriate encoding
- Path traversal: file path construction, directory escapes, symlink following
- Command injection: shell command construction, argument injection
- SQL injection: parameterized queries vs string concatenation
- Unsafe deserialization: untrusted data deserialization, type confusion
- Content type validation: MIME type checks, file magic bytes, extension validation
- Size and rate limits: request size bounds, upload limits, throttling
