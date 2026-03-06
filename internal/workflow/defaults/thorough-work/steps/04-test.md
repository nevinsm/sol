# Test

Run thorough testing for {{issue}}.

1. Run the full test suite — not just the tests you wrote
2. Verify edge cases identified during design are covered
3. Check for regressions in related functionality
4. If any tests fail, fix the root cause and re-run

All tests must pass before advancing.

When the test suite is green, advance:
`sol workflow advance --world=$SOL_WORLD --agent=$SOL_AGENT`
