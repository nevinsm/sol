# Commission Security Fix Caravan

Read all validated security findings, group them into coherent writs, and create a fix caravan in drydock.

## Inputs

1. **`triage.md`** from the triage step — Section 1 (confirmed findings) and Section 3 (cross-cutting patterns)

## Process

### 1. Gather All Confirmed Findings

From triage Section 1, collect all confirmed findings. Note:
- Severity (CRITICAL/HIGH/MEDIUM/LOW)
- CWE classification
- File paths affected
- Whether the finding was previously deferred

### 2. Cross-Reference Prior Caravan (if provided)

{{prior_caravan}} is the caravan ID from a previous security scan. If non-empty:
- Run `sol caravan status {{prior_caravan}}` to see all writs from the prior scan
- For each finding, check if it was already addressed by a prior writ:
  - Compare file paths, CWE IDs, and issue descriptions
  - If the prior writ is merged, the issue is likely fixed — **skip it**
  - If the prior writ failed or was dropped, the issue may still exist — **keep it**
- Document all skip decisions in the disposition log

### 3. Group Findings into Writs

Use judgment to group related findings into coherent writs:

**Group by security domain first:**
- All injection findings in the same package → one writ
- All crypto issues → one writ (unless they span many unrelated packages)
- All file permission issues in the same subsystem → one writ
- Each CVE dependency bump → separate writ (version bumps are atomic)

**Then refine by practicality:**
- **Don't over-group**: a writ that fixes 3 different CWE categories across 10 packages is too broad
- **Don't under-group**: 5 separate writs for the same `math/rand` → `crypto/rand` change in the same package wastes dispatch overhead
- **Systemic patterns**: if triage identified a systemic issue, create one writ for the fix (e.g., "Add input sanitization helper") rather than N per-instance writs
- **Keep writs focused enough for one agent to complete in one session**

### 4. Assign Priority

- **P0 (CRITICAL)**: Exploitable vulnerabilities with concrete attack scenarios — CRITICAL severity findings
- **P1 (HIGH)**: Real vulnerabilities requiring specific conditions to exploit — HIGH severity findings, CVEs with available fixes
- **P2 (MEDIUM)**: Defense-in-depth improvements, MEDIUM severity findings, previously deferred issues
- **P3 (LOW)**: Code hygiene, LOW severity findings, theoretical concerns

### 5. Identify File Conflicts

If two writs touch the same file, they cannot be dispatched in parallel — they'll create merge conflicts. Identify these pairs and sequence them:
- First writ in an earlier phase
- Second writ in the next phase with a dependency on the first
- Document the conflict in both writ descriptions

### 6. Create Writs

For each writ, include enough context for an autonomous agent to execute:
- **What's wrong**: file paths, line numbers, CWE ID, the exact vulnerabilities (quote the code)
- **Why it matters**: concrete exploit scenario or security impact
- **What the fix should look like**: approach, not exact code. Reference secure coding patterns.
- **Acceptance criteria**: how to verify the fix is correct (e.g., "gosec no longer flags G201 on this file", "govulncheck shows no findings for this CVE")
- **Scope boundaries**: "ONLY modify X" and "Do NOT touch Y"
- **Testing requirements**: what tests should verify the fix doesn't break functionality
- **Kind**: `code` for source changes

Create writs using `sol writ create`:
```
sol writ create --world=<world> --title="..." --description="..." --kind=code
```

### 7. Create Caravan in Drydock

Group all writs into a caravan:
```
sol caravan create "security-scan fixes — <date>" <writ-id-1> <writ-id-2> ...
```

Phase assignment:
- **Phase 0**: P0/P1 writs with no file conflicts (critical fixes first)
- **Phase 1**: P1/P2 writs, including conflict partners of phase 0
- **Phase 2**: P2/P3 writs, including conflict partners of phase 1

## Output

### `synthesis.md` — Disposition Log

Every confirmed finding from triage must be dispositioned:
- **Became writ**: finding → writ ID and title
- **Grouped into writ**: finding merged with related findings → writ ID
- **Skipped: prior caravan**: already addressed by prior writ (cite the writ)
- **Skipped: too trivial**: not worth dispatch overhead (explain why)
- **Deferred: needs investigation**: create an analysis writ if warranted

### `caravan.md` — Caravan Summary

- Caravan name and ID
- Writ count by priority (P0/P1/P2/P3)
- Writ count by CWE category
- Phase breakdown with writ list
- File conflict pairs with sequencing rationale
- Full writ list: ID, title, priority, phase, CWE IDs, files touched

### `baseline.json` — Updated Baseline

Read the current baseline from `.sol/workflows/security-scan/baseline.json`, then produce an updated version:
- **Add entries** recommended by triage Section 4 (new false positives, accepted risks)
- **Add entries** for findings skipped during commission (accepted risks, too-trivial-for-writ)
- **Remove entries** for findings that now have fix writs (the fix makes the baseline entry unnecessary)
- **Set `added` date** on new entries to the current date
- Write the complete updated `baseline.json` to the output directory

### `baseline-updates.md` — Baseline Changelog

A human-readable changelog of what changed in the baseline and why:
- New entries added (with justification)
- Entries removed (with fix writ reference)
- Net change summary

### Baseline Update Writ

The commission step MUST create a writ to apply the baseline update. This writ:
- Copies `baseline.json` from the commission output directory to `.sol/workflows/security-scan/baseline.json`
- Add this writ to the fix caravan at **phase 0** (no file conflicts with code fixes — it only touches the workflow config)
- Title: "Update security-scan baseline"
- Kind: `code`

## Constraints

**DO NOT modify any source code.** Your deliverables are writs, a caravan, and disposition logs.

**Every finding must be dispositioned.** No finding should silently disappear between triage and commission.

**Writ descriptions must be self-contained.** Builder agents don't have scan context. Include file paths, line numbers, code quotes, CWE IDs, and concrete acceptance criteria in every writ.

**Scope writs explicitly.** Include "ONLY modify X" and "Do NOT touch Y" boundaries.

**Include security context.** Builder agents need to understand the security rationale, not just "change X to Y." Explain why the current code is vulnerable and why the fix is secure.
