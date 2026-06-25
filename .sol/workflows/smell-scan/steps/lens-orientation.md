# Lens: Orientation Surface Accuracy

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. This lens is the most
direct expression of the scan's north star: it audits the surface a returning
maintainer reads *first* to orient. If that surface is wrong, every downstream
read starts from a false map. Categories: **OPAQUE**, **MISLEAD**. Whole-tree
sweep.

A stale orientation surface is worse than a missing one: a missing package
comment costs a few minutes of reading; a *wrong* CLAUDE.md component index or a
lying ADR sends the maintainer to the wrong place with false confidence. Weight
S1 for actively-misleading surface, S2/S3 for merely-absent.

## What to Audit

### CLAUDE.md accuracy

The Components list, Key Concepts, Conventions, and Design Conventions sections
are the primary map. Check each component entry against the actual package: does
it exist, is the description still true, are the cited paths correct, is the
method/interface count right? Flag entries describing removed or renamed things,
and significant components with no entry.

### ADR currency (`docs/decisions/`)

Spot-check ADRs against the code they govern. Look for: ADRs marked Accepted
whose decision the code no longer follows; ADRs superseded in fact but not in
status; architectural decisions made in code with no ADR (the New Component
Requirements in `principles.md` mandate one). You do not need to read every ADR
end-to-end; sample the ones touching active areas and the ones the component
index references.

### Package doc comments

Does each non-trivial package have a doc comment stating its single
responsibility? A package a maintainer cannot summarize from its doc comment is
an orientation cost. Absent comments on small obvious packages are low priority;
absent or wrong comments on core packages are real.

### Comment-vs-code drift

Sample comments in core packages and check they still describe the code. Flag
comments that describe a prior design (e.g. pre-port behavior), reference removed
functions, or contradict the code. A lying comment is S1: it actively misleads.

### Other orientation docs

`docs/cli.md` (auto-generated; flag if regeneration is overdue and it misleads),
`docs/naming.md` glossary (terms that no longer match the code), `docs/failure-modes.md`
(components missing recovery entries), README-level docs.

## Discipline

Quote the surface text and the contradicting code side by side. "Comment could
be improved" is not a finding; "comment at X:NN says the lock guards Y but it
guards Z" is. Absence is a finding only where the missing surface imposes real
orientation cost on a core area, not for every small package. Note where the doc
is auto-generated (the fix is regeneration + a process note, not a manual edit).

## Out of Scope

- Whether the code *behind* the surface is well-structured: the `structure-*`
  steps and other lenses. You audit the map, not the territory.

Write findings to `review.md` per the DOCTRINE schema.
