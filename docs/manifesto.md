# Manifesto: Building a Production-Ready Multi-Agent Orchestration System

---

## The Problem

AI coding agents are becoming central to software engineering workflows. A single
developer can now coordinate 10, 20, 30 concurrent agents working across multiple
repositories. But coordination at this scale requires infrastructure that doesn't
exist yet — at least not in a stable, production-ready form.

The problems are real and well-understood:

- **Accountability**: Who did what? Which agent touched which code?
- **Durability**: Work must survive crashes, restarts, and context loss.
- **Coordination**: Agents must not step on each other. Merges must be ordered.
  Failures must be detected and recovered.
- **Visibility**: The human overseer must be able to see what's happening,
  intervene when needed, and trust the system to run autonomously when they don't.

## The Prototype

A system called Gastown proved this is possible. It demonstrated that you can:

- Give agents persistent identities that accumulate work history across assignments
- Use git worktrees as isolated sandboxes so agents never conflict
- Attach work to agents via a durability primitive (the "tether") that survives
  session crashes and context compaction
- Build a supervision hierarchy where AI agents monitor other AI agents
- Route completed work through a merge queue with quality gates
- Track batches of work across repositories

Gastown proved the concept. It also proved that getting it right is hard. Across
multiple iterations — JSONL storage, SQLite, Dolt — the system has never been
fully stable. The author calls it experimental. It is a prototype, and prototypes
are not products.

## What We Learned

From studying Gastown's architecture in depth, we extracted principles that work
and identified complexity that doesn't earn its keep.

### Principles Worth Keeping

**Zero Filesystem Cache (ZFC)**: Never cache state in memory. Always derive it
from the source of truth at point of use. With 30 concurrent agents mutating
state, any cache is a lie waiting to happen. This is how Unix tools work — `ls`
reads the directory fresh every time — and it's the right model.

**The Propulsion Principle (GUPP)**: When an agent starts and finds work on its
tether, it executes immediately. No waiting for confirmation, no polling for
instructions. The tether IS the instruction. Idle agents are wasted capacity, and
in a system designed for autonomous operation, waiting is a bug.

**Persistent Identity, Ephemeral Sessions**: An agent's identity (work history,
skill profile, cost accounting) persists indefinitely. Its session (the running
AI process) is disposable and cycles freely. This separation is essential — it
means you can restart, hand off, and recover without losing the agent's
accumulated knowledge.

**Tether Durability**: Work attached to an agent must survive any failure of the
agent's session. The tether is the contract between the system and the agent: "this
is your job, and it will still be your job when you come back."

### Complexity That Doesn't Earn Its Keep

**Universal bus coupling**: Using a single state substrate (beads) for everything
— work items, mail, agent identity, molecules, escalations — creates deep coupling
that makes every subsystem depend on one storage layer. When that layer is
unreliable, everything is unreliable.

**Three-layer supervision**: A dumb daemon spawning an ephemeral AI triage agent
(Boot) to monitor a persistent AI watchdog (Consul) to monitor per-world health
agents (Sentinels) to monitor workers (Outposts). The concept is sound — a hung
process can't detect its own hang — but the implementation has produced real bugs
at every layer boundary.

**188 commands**: Feature accumulation over time. Many commands are slight
variations of others. A production system should have a smaller, more coherent
surface.

## What We're Building

Not a port of Gastown. Not a slavish application of Unix philosophy. A
production-ready system informed by both.

### Production-Ready Means:

- **It works when things break.** Agents crash. Storage hiccups. Sessions die
  mid-work. The system must recover gracefully, not cascade failures.
- **It's inspectable.** The operator can see what every agent is doing, what
  work is pending, what failed and why — without specialized tooling. Files you
  can `cat`. State you can `ls`. Logs you can `tail`.
- **It's simple to operate.** Start it, stop it, add a project, dispatch work.
  No 20-step boot sequence. No database server to nurse.
- **It degrades gracefully.** If the supervision layer is down, agents still
  execute their tethered work. If the merge queue is down, completed work waits
  safely. Nothing is lost.
- **It evolves without breaking.** The system will change over time. Storage
  formats, communication mechanisms, workflow patterns — all must be replaceable
  without rebuilding from scratch.

### Unix Philosophy Means:

Not "everything must be a text stream piped through `awk`." The real Unix
philosophy:

- **Simple tools that do one thing well.** A tether attacher that attaches tethers.
  A session manager that manages sessions. Not a 2000-line monolith that does
  both plus formula instantiation plus caravan creation.
- **Composition over monoliths.** The dispatch operation is a sequence of atomic
  steps. Each step should be independently understandable, testable, and
  replaceable.
- **Text and file interfaces where practical.** State stored as files you can
  read. Messages stored in directories you can list. Not because "files are
  Unix" but because inspectability is a production requirement and files are
  the most inspectable interface humans have.
- **Fail predictably.** Every component has a defined failure mode. When storage
  is down, commands that need storage fail fast with a clear error. Commands
  that don't need storage still work.
- **No hidden magic.** The system's behavior should be traceable from inputs to
  outputs without reading source code. Configuration is explicit. Conventions
  are documented. Side effects are visible.

### Pragmatism Means:

- If 30 concurrent agents need transactional writes, we use a database. Not
  because it's Unix, but because advisory locks on flat files at that concurrency
  level is reinventing a database badly.
- If the operator needs to attach to an agent's session and see what it's doing,
  we use tmux. Not because it's the simplest tool, but because no combination of
  process groups and named pipes gives you interactive debugging.
- If a workflow needs atomic multi-step execution with rollback, we build that
  as a single tool, not a shell pipeline. Transactions are fundamentally
  anti-composition, and pretending otherwise creates fragile systems.

## How We're Building It

**Destination first, then the route.** Design the complete target architecture —
every component, every interface, every failure mode. Then decompose it into
incremental build loops where each loop produces a working system.

**Each loop is a working system.** Not a half-built foundation waiting for the
next loop to become useful. Loop 0 dispatches work to an agent and the agent
executes it. Loop 1 adds multi-agent supervision. Loop 2 adds merge queuing.
Loop 3 adds health monitoring and observability. Each loop can be used, tested,
and validated independently.

**Never throw away work.** Each loop builds on the previous. No "we'll rewrite
this properly later." If it's built, it stays built. This is how stable systems
are made — by accretion of working parts, not by grand rewrites.

**The prototype is our requirements document.** Gastown told us what problems
need solving, which design principles hold up under pressure, and where
complexity accumulates without earning its keep. We don't have to guess what
this system needs. We have a comprehensive behavioral specification extracted
from a working (if unstable) implementation.

**Stability is the feature.** Not more commands. Not more integrations. Not
more configuration options. The system that works reliably with 5 agents is
more valuable than the system that sometimes works with 30.

---

*This manifesto was written during the architectural analysis phase of the
project, after comprehensive documentation of the Gastown prototype and before
the target architecture design. It captures the intent and constraints that
should guide every design decision that follows.*
