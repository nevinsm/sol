# Code Smells Review

Review the code changes for anti-patterns and technical debt.

Examine the branch diff against main. Think about what will cause pain during the next change.

**Look for:**
- Long methods — functions over 50 lines are suspicious, over 100 need justification
- Deep nesting — more than 3 levels of indentation signals need for early returns or extraction
- Shotgun surgery — a single logical change requiring edits in many unrelated files
- Feature envy — a function that uses another package's data more than its own
- Data clumps — the same group of parameters passed together repeatedly
- Primitive obsession — using string/int where a named type would add clarity
- God classes/functions — one type or function that does everything
- Copy-paste code — duplicated logic that should be extracted (DRY violations)
- TODO/FIXME accumulation — new TODOs added without tracking
- Speculative generality — abstractions built for hypothetical future needs
- Temporary fields — struct fields only set in some code paths
- Long parameter lists — more than 4-5 params suggests a config struct

**Questions to answer:**
- What will cause pain during the next change in this area?
- What would you refactor if you owned this code long-term?
- Is technical debt being added or paid down by this change?
- Are there patterns here that will be copy-pasted by future contributors?
- Could any of these smells hide bugs?

Distinguish between smells in new code (should fix) vs pre-existing (note but don't block).
