# Sol CLI JSON API Schemas

## EXPERIMENTAL — schemas may change in any release until sol v1.0

The JSON output schemas documented in this directory are EXPERIMENTAL. They are
documented and contract-tested so we (and any consumers) can detect drift, but
they are NOT yet covered by a stability guarantee. Field names, field shapes,
and enum values may change in any release until sol reaches v1.0. Once sol
reaches v1.0, these schemas become part of the public API per the stability
contract in [docs/integration-api.md](../integration-api.md).

If you are integrating against these schemas today, pin to a specific sol binary
version and re-test on every upgrade.

---

## Overview

The `internal/cliapi/` package defines the canonical Go types for sol's `--json`
output. Each sub-package (e.g. `cliapi/writs`, `cliapi/agents`) contains named
structs with explicit `json:"snake_case"` tags that form the public API surface.
These types wrap internal store types via `From*` conversion functions, decoupling
the JSON contract from storage internals so that store refactors cannot
accidentally break consumers. The schema files in this directory are the JSON
Schema representation of those Go types, generated from the source.

## Schema files

Each command that supports `--json` has a corresponding schema file:

- **Naming**: `<command>.schema.json` — one file per command (e.g.
  `status.schema.json`, `writ-list.schema.json`).
- **Generation**: Schemas are generated from the `cliapi` Go types via
  `make api-schemas`. Do not edit schema files by hand.
- **Field naming rules**:
  - `snake_case` for all JSON keys
  - Timestamps are named `*_at` (e.g. `created_at`, `resolved_at`)
  - Primary entity ID is `id`; foreign references are `<entity>_id`
  - Enums are lowercase strings (`open`, `closed`, `working`, `idle`, ...)
  - Nullable scalars use the type's zero value or are omitted (`omitempty`)
  - Empty arrays are present (not omitted) — `[]`, never `null`
  - All time fields are RFC 3339 strings in UTC

## Available schemas

| Command | Schema file | Human-readable doc |
|---------|-------------|--------------------|
| `account-delete` | [account-delete.schema.json](account-delete.schema.json) | [account-delete.md](account-delete.md) |
| `account-list` | [account-list.schema.json](account-list.schema.json) | [account-list.md](account-list.md) |
| `agent-delete` | [agent-delete.schema.json](agent-delete.schema.json) | [agent-delete.md](agent-delete.md) |
| `agent-sync` | [agent-sync.schema.json](agent-sync.schema.json) | [agent-sync.md](agent-sync.md) |
| `broker-status` | [broker-status.schema.json](broker-status.schema.json) | [broker-status.md](broker-status.md) |
| `caravan-check` | [caravan-check.schema.json](caravan-check.schema.json) | [caravan-check.md](caravan-check.md) |
| `caravan-delete` | [caravan-delete.schema.json](caravan-delete.schema.json) | [caravan-delete.md](caravan-delete.md) |
| `caravan-dep-list` | [caravan-dep-list.schema.json](caravan-dep-list.schema.json) | [caravan-dep-list.md](caravan-dep-list.md) |
| `caravan-launch` | [caravan-launch.schema.json](caravan-launch.schema.json) | [caravan-launch.md](caravan-launch.md) |
| `cast` | [cast.schema.json](cast.schema.json) | [cast.md](cast.md) |
| `chronicle-status` | [chronicle-status.schema.json](chronicle-status.schema.json) | [chronicle-status.md](chronicle-status.md) |
| `consul-status` | [consul-status.schema.json](consul-status.schema.json) | [consul-status.md](consul-status.md) |
| `cost-agent` | [cost-agent.schema.json](cost-agent.schema.json) | [cost-agent.md](cost-agent.md) |
| `cost-caravan` | [cost-caravan.schema.json](cost-caravan.schema.json) | [cost-caravan.md](cost-caravan.md) |
| `cost-world` | [cost-world.schema.json](cost-world.schema.json) | [cost-world.md](cost-world.md) |
| `cost-writ` | [cost-writ.schema.json](cost-writ.schema.json) | [cost-writ.md](cost-writ.md) |
| `doctor` | [doctor.schema.json](doctor.schema.json) | [doctor.md](doctor.md) |
| `forge-await` | [forge-await.schema.json](forge-await.schema.json) | [forge-await.md](forge-await.md) |
| `forge-status` | [forge-status.schema.json](forge-status.schema.json) | [forge-status.md](forge-status.md) |
| `forge-sync` | [forge-sync.schema.json](forge-sync.schema.json) | [forge-sync.md](forge-sync.md) |
| `ledger-status` | [ledger-status.schema.json](ledger-status.schema.json) | [ledger-status.md](ledger-status.md) |
| `prefect-status` | [prefect-status.schema.json](prefect-status.schema.json) | [prefect-status.md](prefect-status.md) |
| `quota-rotate` | [quota-rotate.schema.json](quota-rotate.schema.json) | [quota-rotate.md](quota-rotate.md) |
| `quota-status` | [quota-status.schema.json](quota-status.schema.json) | [quota-status.md](quota-status.md) |
| `schema-migrate` | [schema-migrate.schema.json](schema-migrate.schema.json) | [schema-migrate.md](schema-migrate.md) |
| `sentinel-status` | [sentinel-status.schema.json](sentinel-status.schema.json) | [sentinel-status.md](sentinel-status.md) |
| `status-combined` | [status-combined.schema.json](status-combined.schema.json) | [status-combined.md](status-combined.md) |
| `status-sphere` | [status-sphere.schema.json](status-sphere.schema.json) | [status-sphere.md](status-sphere.md) |
| `status-world` | [status-world.schema.json](status-world.schema.json) | [status-world.md](status-world.md) |
| `workflow-init` | [workflow-init.schema.json](workflow-init.schema.json) | [workflow-init.md](workflow-init.md) |
| `workflow-show` | [workflow-show.schema.json](workflow-show.schema.json) | [workflow-show.md](workflow-show.md) |
| `world-delete` | [world-delete.schema.json](world-delete.schema.json) | [world-delete.md](world-delete.md) |
| `world-export` | [world-export.schema.json](world-export.schema.json) | [world-export.md](world-export.md) |
| `world-status` | [world-status.schema.json](world-status.schema.json) | [world-status.md](world-status.md) |
| `world-sync` | [world-sync.schema.json](world-sync.schema.json) | [world-sync.md](world-sync.md) |
| `writ-clean` | [writ-clean.schema.json](writ-clean.schema.json) | [writ-clean.md](writ-clean.md) |
| `writ-dep-list` | [writ-dep-list.schema.json](writ-dep-list.schema.json) | [writ-dep-list.md](writ-dep-list.md) |
| `writ-trace` | [writ-trace.schema.json](writ-trace.schema.json) | [writ-trace.md](writ-trace.md) |

## How to consume

Pipe any sol command through `--json` to get structured output:

```bash
sol status --json | jq .
```

To validate output against a schema, use the `jsonschema` CLI
([check-jsonschema](https://github.com/python-jsonschema/check-jsonschema)):

```bash
# Install once
pip install check-jsonschema

# Validate live output
sol status --json > /tmp/status.json
check-jsonschema --schemafile docs/api/status.schema.json /tmp/status.json
```

In a Bash script:

```bash
#!/usr/bin/env bash
set -euo pipefail

output=$(sol writ list --world=myworld --json)
count=$(echo "$output" | jq 'length')
echo "Found $count writs"

# Filter open writs
echo "$output" | jq '[.[] | select(.status == "open")]'
```

In Python:

```python
import json
import subprocess

result = subprocess.run(
    ["sol", "writ", "list", "--world=myworld", "--json"],
    capture_output=True, text=True, check=True,
)
writs = json.loads(result.stdout)
open_writs = [w for w in writs if w["status"] == "open"]
```

## How drift is detected

Every `cliapi` sub-package includes contract tests (`*_test.go`) that marshal
sample values to JSON and assert the resulting keys, shapes, and naming
conventions. These tests verify that the Go struct tags produce the expected
JSON contract — if a field is renamed, retyped, or removed, the corresponding
test fails.

Running `make test` executes all contract tests. Any agent or CI pipeline that
runs the test suite will catch schema drift before it reaches consumers.

## How to propose schema changes

Changing the API contract is a deliberate, three-step process:

1. **Modify the `cliapi` type** — update the Go struct in the relevant
   `internal/cliapi/` sub-package.
2. **Regenerate the schema** — run `make api-schemas` to produce the updated
   `.schema.json` file.
3. **Update the contract tests** — adjust the `*_test.go` assertions to match
   the new shape.

All three artifacts change together in the same commit. Reviewers should treat
schema diffs with the same scrutiny as public API changes — they affect every
downstream consumer.
