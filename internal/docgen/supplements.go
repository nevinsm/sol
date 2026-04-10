package docgen

// supplements maps command paths (e.g. "sol caravan") to markdown content
// that is injected immediately after the command's header section (Short +
// Long descriptions) but before the Subcommands table. This lets us add
// curated documentation — diagrams, lifecycle explanations — that cannot be
// generated mechanically from the Cobra command tree.
var supplements = map[string]string{
	"sol caravan": caravanLifecycle,
}

const caravanLifecycle = `#### Caravan Lifecycle

A caravan moves through three states:

` + "```" + `
                  commission
  ┌──────────┐ ──────────────► ┌──────────┐
  │ drydock  │                 │   open   │
  │ (draft)  │ ◄────────────── │(dispatch)│
  └──────────┘    drydock      └──────────┘
    ▲   │                         │
    │   │ delete                  │ close (--confirm)
    │   ▼                         ▼
    │  [deleted]               ┌──────────┐
    │                          │  closed  │
    └────────── reopen ─────── │(archived)│
                               └──────────┘
                                  │
                                  │ delete
                                  ▼
                               [deleted]
` + "```" + `

- **drydock** — The initial state after ` + "`sol caravan create`" + `. The caravan is a draft: you can add/remove items and set phases, but it cannot be dispatched. Think of it as the staging area.
- **open** — Reached via ` + "`sol caravan commission`" + `. The caravan is now live and dispatchable — ` + "`sol caravan launch`" + ` will cast ready items. Items can still be added while open.
- **closed** — Reached via ` + "`sol caravan close --confirm`" + `. The caravan is archived. No further dispatches occur. Use this when all work is complete or the batch is abandoned.

Transitions: ` + "`create`" + ` → drydock; ` + "`commission`" + ` → open; ` + "`close`" + ` → closed; ` + "`reopen`" + ` → drydock; ` + "`drydock`" + ` → drydock (from open); ` + "`delete`" + ` removes a drydocked or closed caravan permanently.

`
