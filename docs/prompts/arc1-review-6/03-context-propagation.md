# Prompt 03: Arc 1 Review-6 — Context Propagation in Consul and Forge

You are threading `context.Context` through the consul patrol loop and forge gate runner so these operations can be cancelled by the parent process.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 02 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/sentinel/sentinel.go` — `Patrol(ctx context.Context)` as the reference pattern for context-aware patrol
- `internal/consul/consul.go` — `Run()`, `Patrol()`, and all patrol sub-functions
- `internal/consul/consul_test.go` — existing consul tests
- `internal/forge/toolbox.go` — `RunGates()` function
- `internal/forge/forge.go` — `Config` struct, `DefaultConfig()`
- `internal/forge/forge_test.go` — existing forge tests
- `cmd/forge.go` — `forgeRunGatesCmd` (caller of `RunGates`)

---

## Task 1: Thread context through `Consul.Patrol()`

**File:** `internal/consul/consul.go`

### Step 1a: Change `Patrol` signature

Change the `Patrol` method signature to accept a context:

```go
func (d *Consul) Patrol(ctx context.Context) error {
```

### Step 1b: Update sub-function signatures

Thread context through each patrol sub-function. These functions currently don't use context, but accepting it now establishes the pattern for future use (e.g., context-aware DB queries, cancellation checks):

```go
func (d *Consul) recoverStaleTethers(ctx context.Context) (int, error) {
func (d *Consul) feedStrandedCaravans(ctx context.Context) (int, error) {
func (d *Consul) processLifecycleRequests(ctx context.Context) (shutdown bool, err error) {
```

Update the call sites within `Patrol` to pass `ctx`:

```go
	recovered, err := d.recoverStaleTethers(ctx)
	// ...
	fed, err := d.feedStrandedCaravans(ctx)
	// ...
	shutdown, err = d.processLifecycleRequests(ctx)
```

### Step 1c: Update `Run()` to pass context to `Patrol`

In the `Run` method, pass the context from the `Run` parameter:

```go
	// Patrol immediately.
	if errors.Is(d.Patrol(ctx), errShutdown) {
		shutdown()
		return nil
	}

	// ...

		case <-ticker.C:
			if errors.Is(d.Patrol(ctx), errShutdown) {
				shutdown()
				return nil
			}
```

### Step 1d: Update tests

Update all test call sites that invoke `Patrol()` directly to pass a context:

```go
err := consul.Patrol(context.Background())
```

Search for `\.Patrol()` in `internal/consul/consul_test.go` and update every call.

---

## Task 2: Thread context through `forge.RunGates()`

**File:** `internal/forge/toolbox.go`

### Step 2a: Change `RunGates` signature

```go
func (r *Forge) RunGates(ctx context.Context) ([]GateResult, error) {
```

### Step 2b: Use parent context for timeouts

Replace `context.Background()` with the parent context:

```go
	for _, gate := range r.cfg.QualityGates {
		start := time.Now()
		gateCtx, cancel := context.WithTimeout(ctx, timeout)
		cmd := exec.CommandContext(gateCtx, "sh", "-c", gate)
```

This way if the parent context is cancelled (e.g., session stops), all running gates are also cancelled.

### Step 2c: Update callers

**File:** `cmd/forge.go`, `forgeRunGatesCmd` (around line 454)

Pass `cmd.Context()`:

```go
		results, err := ref.RunGates(cmd.Context())
```

Search for any other callers of `RunGates` in the codebase (e.g., in forge session toolbox usage) and update them too. If called from a context that already has one available, pass it through. If not, use `context.Background()` as a placeholder — but add a `// TODO: propagate context` comment.

---

## Task 3: Tests

### Consul tests

**File:** `internal/consul/consul_test.go`

Update all `Patrol()` calls to pass `context.Background()`. No new test functions needed — this is a signature change.

### Forge tests

**File:** `internal/forge/forge_test.go`

Update all `RunGates()` calls to pass `context.Background()`. If there's an existing test that verifies gate execution, update it.

Optionally add a cancellation test:

```go
func TestRunGatesCancelledContext(t *testing.T)
```
- Create a forge with a gate that sleeps (e.g., `"sleep 10"`)
- Create a context with a very short timeout (100ms)
- Call `RunGates(ctx)` — should return quickly with the gate marked as failed
- Verify elapsed time is well under 10 seconds

Only add this test if it's straightforward with the existing test helpers. If it requires significant new infrastructure, skip it.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Grep for `\.Patrol()` (no args) — should find zero results in non-test Go files
- Grep for `context.Background()` in `toolbox.go` — should find zero results (replaced by parent context)

## Commit

```
fix(consul,forge): arc 1 review-6 — context propagation in patrol and gate runner
```
