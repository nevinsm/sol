# Document Findings and Design Fixes

Write up your findings and design the fix approach.

For each root cause identified, document:
1. **What** is wrong
2. **Where** it is (file:line)
3. **Why** it happens
4. **How** to fix it

Break the fixes into discrete work items:
- Each item should be independently implementable and testable
- Consider dependencies between fixes — does fix B require fix A to land first?
- Think about phasing and ordering
- Write titles and descriptions clearly enough that an agent can execute them
  without additional context

When you have a complete set of fix items designed, advance:
`sol workflow advance --world=$SOL_WORLD --agent=$SOL_AGENT`
