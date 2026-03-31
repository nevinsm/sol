# Run gosec — Raw Output Collection

Run gosec once across the entire codebase with all rules enabled. Save raw output for downstream analysis steps. Do NOT perform any analysis — this step is purely tool execution.

## Tool Installation

If gosec is not installed, install it:

```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

## Execution

Run gosec with all rules (no `-include` filter) and JSON output:

```bash
gosec -fmt=json -quiet ./... 2>gosec-stderr.txt
```

Save the raw JSON output to `gosec-raw.json` in your writ output directory.
Save stderr to `gosec-stderr.txt` in your writ output directory (captures tool warnings and errors).

## Output

Two files in your writ output directory:

- **`gosec-raw.json`** — complete gosec JSON output with all findings across all rules
- **`gosec-stderr.txt`** — stderr from the gosec run (tool errors, warnings)

## Constraints

**DO NOT analyze findings.** Downstream steps handle analysis.

**DO NOT filter by rule.** Run with all rules enabled — analysis steps filter the shared output.

**DO NOT modify any source code.** This is a read-only tool execution step.

**Run gosec exactly once.** The whole point of this step is to avoid redundant gosec invocations.
