package events

// Lifecycle event convention.
//
// A "lifecycle event" is a daemon's record of its own start, stop, or
// fatal error. These events are operator-facing audit records — they
// describe the daemon process itself, not the work it did.
//
// All lifecycle events emitted by sphere daemons share the same shape so
// consumers (status tools, audit log filters, dashboards) can filter
// uniformly across components:
//
//   source     = component name (e.g. "chronicle", "ledger")
//   actor      = component name (same as source — the daemon emits about itself)
//   visibility = "audit"          (lifecycle is operator-facing, not feed surface)
//
// Per-component events that describe ongoing work — patrol summaries,
// ingest summaries, action records — are NOT lifecycle events. Those use
// visibility="feed" or "both" as appropriate and may have richer source
// or actor strings.
//
// Use EmitLifecycle to enforce the convention rather than calling
// Logger.Emit directly with hand-rolled visibility strings.

// EmitLifecycle emits a daemon lifecycle event with the uniform
// source/actor/visibility shape. See the package comment for the
// convention.
//
// component is the daemon's name (e.g. "chronicle", "ledger"). Both
// source and actor are set to component because lifecycle events are
// self-emitted: the daemon is reporting about itself.
//
// If logger is nil this is a no-op (matches the existing optional-logger
// pattern in the daemons that call this helper).
func EmitLifecycle(logger *Logger, eventType, component string, payload any) {
	if logger == nil {
		return
	}
	logger.Emit(eventType, component, component, "audit", payload)
}
