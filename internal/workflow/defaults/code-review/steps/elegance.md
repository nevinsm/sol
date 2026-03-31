# Elegance Review

Review the code changes for design clarity and abstraction quality.

Examine the branch diff against main. Consider maintainability and readability.

**Look for:**
- Unclear abstractions or naming — does the name reveal intent?
- Functions doing too many things — violating single responsibility
- Missing abstractions — repeated patterns that should be extracted
- Over-engineered abstractions — unnecessary indirection or generality
- Coupling that should be loose — concrete types where interfaces would be better
- Dependencies that flow the wrong direction — lower layers importing higher
- Unclear data flow or control flow — hard to trace execution
- Magic numbers/strings without named constants or explanation
- Inconsistent design patterns within the codebase
- Violation of SOLID principles
- Reinventing existing utilities — standard library or project helpers already exist

**Questions to answer:**
- Would a new team member understand this code without asking questions?
- Does the structure match the problem domain?
- Is the complexity justified by the requirements?
- Are the right things public vs private?
- Does the abstraction level stay consistent within each function?

Focus on actionable suggestions, not style preferences.
