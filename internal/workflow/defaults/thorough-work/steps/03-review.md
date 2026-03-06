# Review

Review your own implementation for {{issue}} critically.

1. Re-read every changed file end to end — not just your diffs
2. Check for correctness: does the logic handle all cases from your design?
3. Check for style: naming, formatting, consistency with surrounding code
4. Check for safety: no security issues, no data loss paths, proper error handling
5. Check for scope: no unrelated changes, no premature abstractions
6. Fix any issues you find before advancing

Treat this as if you are reviewing someone else's code.

When the review is clean, advance:
`sol workflow advance --world=$SOL_WORLD --agent=$SOL_AGENT`
