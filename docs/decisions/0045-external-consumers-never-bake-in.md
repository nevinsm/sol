# ADR-0045: External Consumers Never Bake Into Sol

Status: Accepted

Date: 2026-08-20

## Context

Sol supports external applications that consume it: chat bridges, dashboards,
automations, and other tooling an operator builds around their installation.
The supported architecture is a standalone program in its own world and repo
that consumes sol exclusively through the CLI's JSON surface and the feed
cursor contract (ADR-0043), attributing its actions via `SOL_VIA`.

Each new consumer resurfaces the same temptation: give it presence inside sol.
A status line so `sol status` shows the consumer is up. A config block for its
settings. A doctor check for its infrastructure. Individually these are small
and convenient. Collectively they turn sol into an inventory of one operator's
private deployment.

Sol is a public-facing project. Someone cloning it should find a generic
multi-agent orchestration system, not references to any particular
installation's surrounding tooling.

## Options Considered

1. **Per-consumer integration points.** Status lines, config keys, or doctor
   checks added as each consumer appears. Rejected: couples a public codebase
   to private deployments, grows sol linearly with consumer count, and every
   integration point is surface that must be tested, documented, and one day
   deprecated.
2. **A generic "external services" registry.** Sol pings operator-listed URLs
   or units and reports their health, keeping the names in config rather than
   code. Rejected: this makes sol a general-purpose service monitor, which is
   scope creep into a solved problem (systemd, existing monitoring), and the
   registry still exists only to serve particular deployments.
3. **Nothing bakes in.** Consumers are invisible to sol. Chosen.

## Decision

External applications never bake into sol. Sol encodes nothing non-generic.

Prohibited, regardless of how small or convenient:

- Status lines, health checks, or dashboard sections naming a consumer.
- Config keys, defaults, or documentation sections that exist for a consumer.
- Code paths keyed on a consumer's identity.
- Doctor checks for consumer infrastructure.

Permitted: generic capability surface that any consumer could use, when the
underlying concept is sol's own. The feed cursor contract, `--json` flags,
exit-code conventions, and (for example) a read-only command for listing an
envoy's memory or persona files are all sol concepts that happen to serve
consumers. The litmus test: **generic concept, yes; named consumer, never.**
A feature request arriving from a consumer is admissible only in its fully
generalized form, and a writ asking for consumer-specific support should be
restated as the generic capability or rejected.

`SOL_VIA` is the one place a consumer's name appears, and it is the exception
that proves the rule: it is free-form attribution *data* flowing through sol's
events and mail, not sol knowing the consumer exists.

Consumers monitor themselves: their own service unit, their own logs, their
own health endpoint. A consumer that wants a unified "whole stack" view builds
it on its own side of the boundary, where it can display sol's health alongside
its own.

## Consequences

- Sol remains publishable as-is. No release hygiene pass is needed to strip
  deployment-specific references, because none exist.
- Consumer count does not grow sol. Ten consumers and zero consumers produce
  the same codebase.
- Pressure from consumers lands on the generic surface, which is where it
  improves sol for everyone: ADR-0043 exists because real consumers needed a
  stable contract, and every gap a consumer finds becomes a generic `--json`
  or CLI improvement rather than a bespoke hook.
- Accepted cost: `sol status` will never answer "is my whole stack up." That
  view lives outside sol, assembled from tools that already do it well.
