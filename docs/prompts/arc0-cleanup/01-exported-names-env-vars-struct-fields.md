# Arc 0 Cleanup, Prompt 1: Exported Names, Env Vars, Struct Fields

## Context

Arc 0 renamed the system from `gt` to `sol`. Post-review found exported struct fields, type names, and environment variables still using old naming. These are breaking changes — renaming a struct field or exported type requires updating all consumers atomically.

The codebase compiles and tests pass. Every change here is a rename of an exported identifier or env var string — no behavioral change.

## What To Change

### 1. GTHome → SolHome (struct field rename)

**File:** `internal/consul/deacon.go`
- Line 24: struct field `GTHome string` → `SolHome string`
- Line 130: `d.config.GTHome` → `d.config.SolHome`
- Line 209: `d.config.GTHome` → `d.config.SolHome`

**File:** `internal/sentinel/witness.go`
- Line 27: struct field `GTHome string` → `SolHome string`
- Line 39: `GTHome: gtHome,` → `SolHome: gtHome,` (the parameter name `gtHome` changes in prompt 2)

**File:** `cmd/consul.go`
- Line 54: `GTHome: config.Home(),` → `SolHome: config.Home(),`

**Required test fixups** (these files set the struct field directly):

**File:** `internal/consul/deacon_test.go`
- All `GTHome:` field references → `SolHome:` (~7 occurrences, use replace_all)

**File:** `internal/sentinel/witness_test.go`
- Line 135: `GTHome: os.Getenv("SOL_HOME"),` → `SolHome: os.Getenv("SOL_HOME"),`

**File:** `test/integration/loop5_test.go`
- All `GTHome:` field references → `SolHome:` (~8 occurrences, use replace_all)

### 2. RefineryClaudeMD* → ForgeClaudeMD* (exported type + function renames)

**File:** `internal/protocol/claudemd.go`
- Line 78 comment: `RefineryClaudeMDContext holds the fields used to generate a CLAUDE.md for the forge.` → `ForgeClaudeMDContext holds the fields used to generate a CLAUDE.md for the forge.`
- Line 79: `type RefineryClaudeMDContext struct` → `type ForgeClaudeMDContext struct`
- Line 86 comment: `GenerateRefineryClaudeMD returns the contents` → `GenerateForgeClaudeMD returns the contents`
- Line 87: `func GenerateRefineryClaudeMD(ctx RefineryClaudeMDContext) string` → `func GenerateForgeClaudeMD(ctx ForgeClaudeMDContext) string`
- Line 173 comment: `InstallRefineryClaudeMD writes .claude/CLAUDE.md` → `InstallForgeClaudeMD writes .claude/CLAUDE.md`
- Line 174: `func InstallRefineryClaudeMD(worktreeDir string, ctx RefineryClaudeMDContext) error` → `func InstallForgeClaudeMD(worktreeDir string, ctx ForgeClaudeMDContext) error`
- Line 180: `content := GenerateRefineryClaudeMD(ctx)` → `content := GenerateForgeClaudeMD(ctx)`

**File:** `cmd/forge.go`
- Line 82: `protocol.RefineryClaudeMDContext{` → `protocol.ForgeClaudeMDContext{`
- Line 88: `protocol.InstallRefineryClaudeMD(` → `protocol.InstallForgeClaudeMD(`

**Required test fixup:**

**File:** `internal/protocol/protocol_test.go`
- Rename test functions: `TestGenerateRefineryClaudeMD` → `TestGenerateForgeClaudeMD`, `TestInstallRefineryClaudeMD` → `TestInstallForgeClaudeMD`
- All `RefineryClaudeMDContext` → `ForgeClaudeMDContext`
- All `GenerateRefineryClaudeMD` → `GenerateForgeClaudeMD`
- All `InstallRefineryClaudeMD` → `InstallForgeClaudeMD`

### 3. GT_ESCALATION_WEBHOOK → SOL_ESCALATION_WEBHOOK

**File:** `cmd/consul.go`
- Line 48: `os.Getenv("GT_ESCALATION_WEBHOOK")` → `os.Getenv("SOL_ESCALATION_WEBHOOK")`

**File:** `cmd/escalate.go`
- Line 44: `os.Getenv("GT_ESCALATION_WEBHOOK")` → `os.Getenv("SOL_ESCALATION_WEBHOOK")`

## What NOT To Change

- Parameter names like `gtHome` — prompt 2
- Test local variables — prompts 3-4
- Any file not listed above

## Acceptance Criteria

```bash
make build && make test     # passes

# No old exported names:
grep -rn 'GTHome' --include='*.go' .               # no hits
grep -rn 'GT_ESCALATION' --include='*.go' .         # no hits
grep -rn 'RefineryClaudeMD' --include='*.go' .      # no hits
```
