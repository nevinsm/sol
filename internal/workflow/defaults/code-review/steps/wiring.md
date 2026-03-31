# Wiring Review

Detect dependencies, configs, or libraries that were added but not actually wired in.

This catches subtle bugs where the implementer THINKS they integrated something,
but the old implementation is still being used.

Examine the branch diff against main. Cross-reference additions with actual usage.

**Look for:**
- New dependency in go.mod but never imported in any .go file
- Package imported but only used in a subset of intended call sites
- SDK or library added but old manual implementation remains
  - e.g., added a logging library but still using fmt.Println for errors
  - e.g., added a validation library but still using manual if-checks
- Config/env var defined but never loaded or referenced
  - New entry in world.toml schema but not read by any code
  - Environment variable documented but not accessed via os.Getenv or config
- Feature flags defined but never checked
- Struct fields added but never set or read
- Interface methods defined but not implemented (or implemented but not called)
- Database migrations that add columns never referenced in queries

**Questions to answer:**
- Is every new dependency actually imported and used?
- Are there old patterns that should have been replaced by the new dependency?
- Is there dead config that suggests an incomplete migration?
- Do all new struct fields participate in the logic?
- Are all new interface methods exercised?

Wiring gaps often indicate incomplete work — flag them clearly.
