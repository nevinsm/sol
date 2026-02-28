# Prompt 04: Arc 1 Review — Documentation Fixes and Final Acceptance

You are fixing documentation inconsistencies found during the Arc 1
review and running a final acceptance sweep.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review prompts 01–03 are complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Fix `docs/arc-roadmap.md` — world list discovery mechanism

**File:** `docs/arc-roadmap.md`

Line 29 says:

> `sol world list` — discover all worlds from `.store/` directory

The actual implementation uses the `worlds` table in sphere.db (via
`sphereStore.ListWorlds()`), not filesystem scanning. This matches
ADR-0008 line 61 which says the `worlds` table is "Used for
`sol world list` and discovery."

Fix line 29 to match reality:

```
- `sol world list` — list all registered worlds from sphere database
```

---

## Task 2: Update `docs/naming.md` — retire Charter reservation

**File:** `docs/naming.md`

Line 38 reserves "Charter" for a per-world configuration file:

> | **Charter** | *(Reserved for future use — per-world configuration file.)* | — |

Arc 1 has implemented this concept as `world.toml` / `WorldConfig`.
Update the row to reflect reality:

```
| **Charter** | Per-world configuration file (`world.toml`). Defines source repo, agent capacity, model tier, and forge settings. Layered with global `sol.toml`. | `world.toml` |
```

---

## Task 3: Fix ADR-0008 gate description

**File:** `docs/decisions/0008-world-lifecycle.md`

The "Hard gate" section says:

> The gate checks for `world.toml` existence — a file check, not a
> database query.

The actual implementation in `config.RequireWorld` also validates the
world name format before the file check. Update to:

> The gate validates the world name format and checks for `world.toml`
> existence — a regex and file check, not a database query.

---

## Task 4: Acceptance Sweep

Run the full acceptance sweep to verify all review changes are correct.

### Build and test

```bash
make build && make test
```

### Bug fixes verified

```bash
export SOL_HOME=/tmp/sol-review-accept
mkdir -p /tmp/sol-review-accept/.store

# B1: world list --json with no worlds → valid JSON
bin/sol world list --json
# → []

# B2: caravan launch uses config source repo (verified by test in prompt 01)
```

### Validation verified

```bash
# Invalid model tier
bin/sol world init validworld --source-repo=/tmp
cat /tmp/sol-review-accept/validworld/world.toml
# Write invalid tier
cat > /tmp/sol-review-accept/validworld/world.toml <<'EOF'
[agents]
model_tier = "invalid"
EOF
bin/sol world status validworld 2>&1
# → error about model_tier

# Reserved name
bin/sol world init store 2>&1
# → error: reserved

# Invalid source repo path
bin/sol world init badrepo --source-repo=/nonexistent/path 2>&1
# → error: no such file or directory

# Negative capacity
cat > /tmp/sol-review-accept/validworld/world.toml <<'EOF'
[agents]
capacity = -1
EOF
bin/sol world status validworld 2>&1
# → error about capacity
```

### Delete hardening verified

```bash
# Clean world
bin/sol world init deltest --source-repo=/tmp
bin/sol store create --world=deltest --title="Test item"
bin/sol agent create --world=deltest --name=TestAgent --role=dev
bin/sol world delete deltest --confirm
# → succeeds
test ! -f /tmp/sol-review-accept/.store/deltest.db && echo "PASS: DB removed"
test ! -d /tmp/sol-review-accept/deltest && echo "PASS: dir removed"
# Verify no orphaned agents
sqlite3 /tmp/sol-review-accept/.store/sphere.db \
  "SELECT COUNT(*) FROM agents WHERE world='deltest'"
# → 0
```

### Status dedup verified

```bash
bin/sol world init statustest --source-repo=/tmp
bin/sol world status statustest
# → should show Config section AND all standard status sections
#   including caravans (if any), merge queue, summary, health
bin/sol status statustest
# → should show same standard sections (without config)
```

### Hard gate coverage verified

```bash
# Table-driven test covers all command families
go test ./test/integration/ -run TestHardGateAllCommands -v
```

### Documentation verified

```bash
# Check roadmap mentions sphere database, not .store/ directory
grep "world list" docs/arc-roadmap.md
# → should mention "sphere database" or "registered worlds"

# Check naming.md Charter is no longer reserved
grep "Charter" docs/naming.md
# → should show world.toml description, not "reserved for future use"

# Check ADR-0008 mentions name validation
grep -i "name" docs/decisions/0008-world-lifecycle.md | head -5
# → should mention "validates the world name format"
```

### Cleanup

```bash
rm -rf /tmp/sol-review-accept
```

---

## Task 5: Grep verification

Run these greps to verify no issues remain:

```bash
# No silent time.Parse errors in store layer
grep -rn 'time.Parse.*_' internal/store/*.go
# → should have no matches (all errors now handled)

# No DiscoverSourceRepo in cmd/ except as fallback inside ResolveSourceRepo
grep -rn 'DiscoverSourceRepo' cmd/*.go
# → should have no matches (caravan launch was the last holdout)

# All status display goes through shared function
grep -rn 'Prefect: running\|Prefect: not running' cmd/*.go
# → should only appear in status.go (the shared printWorldStatus function)
```

---

## Guidelines

- Documentation changes are minimal — fix only the specific issues
  identified. Do not rewrite surrounding text.
- The acceptance sweep must pass completely before committing.
- If any verification step fails, fix the issue before proceeding.
- Commit with message:
  `docs(world): arc 1 review — fix roadmap, naming glossary, ADR-0008`
