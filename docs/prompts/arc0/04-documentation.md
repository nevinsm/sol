# Arc 0, Prompt 4: Documentation Rename

## Context

We are renaming the `gt` system to `sol`. Prompts 1-3 renamed all Go code — module, binary, environment, store, tether, dispatch, and all process components. This final prompt updates all documentation to reflect the new naming.

Read `docs/naming.md` for the full naming glossary and migration reference table.

**State after prompt 3:** All Go code uses new names. `make build && make test` passes. Binary is `bin/sol`. All commands use new names (cast, resolve, prefect, forge, sentinel, chronicle, consul, caravan).

## Complete Rename Reference

Apply these substitutions across all documentation files. Use the context to avoid false positives (e.g., don't replace "rig" inside "trigger" or "original").

### System & Binary
| Old | New |
|---|---|
| `gt` (the system/binary) | `sol` |
| `bin/gt` | `bin/sol` |
| `GT_HOME` | `SOL_HOME` |
| `GT_RIG` | `SOL_WORLD` |
| `GT_AGENT` | `SOL_AGENT` |
| `~/gt` (default home) | `~/sol` |
| `/tmp/gt-test` | `/tmp/sol-test` |
| `github.com/nevinsm/gt` | `github.com/nevinsm/sol` |

### Concepts
| Old | New |
|---|---|
| rig (project/workspace) | world |
| polecat (worker agent) | outpost |
| town (global registry) | sphere |
| hook (durability file) | tether |
| sling (dispatch) | cast |
| done (signal complete) | resolve |
| convoy (batch work) | caravan |

### Processes
| Old | New |
|---|---|
| supervisor | prefect |
| refinery | forge |
| witness | sentinel |
| curator | chronicle |
| deacon | consul |

### File/Directory Paths
| Old | New |
|---|---|
| `polecats/` | `outposts/` |
| `.hook` | `.tether` |
| `town.db` | `sphere.db` |
| `{rig}.db` | `{world}.db` |
| `refinery/` (worktree dir) | `forge/` |

### Session Names
| Old | New |
|---|---|
| `gt-{rig}-{agent}` | `sol-{world}-{agent}` |
| `gt-supervisor` | `sol-prefect` |
| `gt-refinery-{rig}` | `sol-forge-{world}` |
| `gt-witness-{rig}` | `sol-sentinel-{world}` |
| `gt-curator` | `sol-chronicle` |
| `gt-deacon` | `sol-consul` |

### IDs
| Old | New |
|---|---|
| `gt-` prefix (work items) | `sol-` |
| `convoy-` prefix | `car-` |

### CLI Commands
| Old | New |
|---|---|
| `gt sling` | `sol cast` |
| `gt done` | `sol resolve` |
| `gt supervisor` | `sol prefect` |
| `gt refinery` | `sol forge` |
| `gt witness` | `sol sentinel` |
| `gt curator` | `sol chronicle` |
| `gt deacon` | `sol consul` |
| `gt convoy` | `sol caravan` |
| `gt prime` | `sol prime` |
| `gt agent` | `sol agent` |
| `gt store` | `sol store` |
| `gt session` | `sol session` |
| `gt status` | `sol status` |
| `gt mail` | `sol mail` |
| `gt feed` | `sol feed` |
| `gt log-event` | `sol log-event` |
| `gt escalate` | `sol escalate` |
| `gt escalation` | `sol escalation` |
| `gt handoff` | `sol handoff` |
| `gt workflow` | `sol workflow` |
| `--rig` flag | `--world` flag |
| `--db` flag (for rig) | `--world` flag |
| `polecat-work` formula | `default-work` formula |

### Branch Convention
| Old | New |
|---|---|
| `polecat/{agent}/{item}` | `outpost/{agent}/{item}` |

### ADR-Specific Renames
| Old | New |
|---|---|
| ADR-0001: Witness as Go process | ADR-0001: Sentinel as Go process |
| ADR-0002: Refinery as Go process | ADR-0002: Forge as Go process |
| ADR-0003: AI assessment (witness) | ADR-0003: AI assessment (sentinel) |
| ADR-0004: Curator as separate component | ADR-0004: Chronicle as separate component |
| ADR-0005: Refinery as Claude session | ADR-0005: Forge as Claude session |
| ADR-0006: Supervisor defers to witness | ADR-0006: Prefect defers to sentinel |
| ADR-0007: Deacon as Go process | ADR-0007: Consul as Go process |

## Files To Update

### Living Documentation (high priority)

1. **`README.md`** — Title, all examples, env vars, commands, session names, paths, Quick Start guide
2. **`CLAUDE.md`** — Title, build/test/install commands, Key Concepts section, Conventions section, all examples
3. **`docs/target-architecture.md`** — All references throughout the full spec
4. **`docs/manifesto.md`** — All references to system name and concepts
5. **`docs/naming.md`** — Verify already correct (should be, it was written with new names). Update any remaining old references.
6. **`docs/arc-roadmap.md`** — Verify already correct. Update any `gt` references that slipped in.

### ADRs (update to new naming)

7. **`docs/decisions/0001-witness-as-go-process.md`** — witness → sentinel, rig → world, etc.
8. **`docs/decisions/0002-refinery-as-go-process.md`** — refinery → forge. Note: this ADR is superseded by 0005, but still update naming.
9. **`docs/decisions/0003-ai-assessment-gated-by-output-hashing.md`** — witness → sentinel
10. **`docs/decisions/0004-curator-as-separate-component.md`** — curator → chronicle
11. **`docs/decisions/0005-refinery-claude-session.md`** — refinery → forge, all command references
12. **`docs/decisions/0006-supervisor-defers-to-witness.md`** — supervisor → prefect, witness → sentinel, polecat → outpost
13. **`docs/decisions/0007-deacon-as-go-process.md`** — deacon → consul

### Loop Prompts (update to new naming)

These are historical build instructions. Update all naming so they're consistent if anyone references them.

**Loop 0:**
14. `docs/prompts/loop0/01-scaffold-store.md`
15. `docs/prompts/loop0/02-session-manager.md`
16. `docs/prompts/loop0/03-dispatch-pipeline.md`
17. `docs/prompts/loop0/04-integration.md`

**Loop 1:**
18. `docs/prompts/loop1/01-name-pool-dispatch.md`
19. `docs/prompts/loop1/02-supervisor.md`
20. `docs/prompts/loop1/03-status-command.md`
21. `docs/prompts/loop1/04-integration.md`

**Loop 2:**
22. `docs/prompts/loop2/01-merge-request-store.md`
23. `docs/prompts/loop2/02-refinery.md`
24. `docs/prompts/loop2/03-cli-commands.md`
25. `docs/prompts/loop2/04-integration.md`

**Loop 3:**
26. `docs/prompts/loop3/01-mail-system.md`
27. `docs/prompts/loop3/02-event-feed.md`
28. `docs/prompts/loop3/03-curator.md`
29. `docs/prompts/loop3/04-witness.md`
30. `docs/prompts/loop3/05-integration.md`
31. `docs/prompts/loop3/06-review-fixes.md`

**Loop 4:**
32. `docs/prompts/loop4/01-workflow-engine.md`
33. `docs/prompts/loop4/02-convoys.md`
34. `docs/prompts/loop4/03-integration.md`
35. `docs/prompts/loop4/acceptance.md`

**Loop 5:**
36. `docs/prompts/loop5/01-escalation-system.md`
37. `docs/prompts/loop5/02-handoff.md`
38. `docs/prompts/loop5/03-deacon.md`
39. `docs/prompts/loop5/04-integration.md`
40. `docs/prompts/loop5/acceptance.md`

### Acceptance Test Documents

41. `test/integration/LOOP0_ACCEPTANCE.md`
42. `test/integration/LOOP1_ACCEPTANCE.md`
43. `test/integration/LOOP2_ACCEPTANCE.md`
44. `test/integration/LOOP3_ACCEPTANCE.md`

### Arc 0 Prompts (self-referential)

45. `docs/prompts/arc0/01-module-binary-environment.md` — These reference the old naming by design (they describe what to change). Leave as-is — they are the rename instructions themselves and should preserve the old→new mapping.
46. `docs/prompts/arc0/02-store-tether-dispatch.md` — Same, leave as-is.
47. `docs/prompts/arc0/03-process-components.md` — Same, leave as-is.
48. This file (`docs/prompts/arc0/04-documentation.md`) — Leave as-is.

## Approach

For each file:
1. Read the file
2. Apply all substitutions from the reference table above
3. Be careful with word boundaries — "rig" must not be replaced inside "trigger", "original", "right", "rigor", "configure", etc.
4. "hook" must not be replaced inside "webhook"
5. "town" should only be replaced when it refers to the gt town concept (database, store), not when used as a generic English word
6. Verify the result reads naturally

## Acceptance Criteria

```bash
make build && make test    # still passes (no code changes in this prompt)

# Verify no old naming in living docs:
grep -rn 'GT_HOME\|GT_RIG\|GT_AGENT' README.md CLAUDE.md docs/target-architecture.md docs/manifesto.md  # no hits
grep -rn 'bin/gt\b' README.md CLAUDE.md docs/target-architecture.md docs/manifesto.md  # no hits
grep -rn '"gt ' README.md CLAUDE.md  # no hits (as command invocations)

# Spot-check key documents:
# - README.md title should be "# Sol —" not "# gt —"
# - CLAUDE.md should reference "make build" producing bin/sol
# - CLAUDE.md Key Concepts should use world, tether, cast, etc.
# - ADRs should use new component names
```
