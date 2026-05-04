// Package softfail provides tiny helpers for "log + continue" error sites.
//
// Many sol code paths intentionally treat errors as soft failures: a missing
// optional file, an unexpected on-disk shape, a best-effort discovery scan.
// The correct behavior is to keep going with a sensible fallback, but the
// implementation must not silently discard the error — that turns recoverable
// problems (permission denied, corrupted state) into silent misbehavior.
//
// Use [Log] for intra-package soft failures where a developer reading logs is
// the only consumer. Use [Emit] at cross-package boundaries where a downstream
// consumer (chronicle, sol feed, audit) needs structured visibility — the
// swallowed error in package A may be the only signal a consumer in package B
// would otherwise have had.
//
// See pattern P1 ("error swallowing in error paths") in the failure-mode
// catalog. The original Log helper landed in writ sol-18364486adf34c8d. The
// Emit variant addresses the cross-domain Pattern 1 verdict (writ
// sol-de5cb1eeb5408930) which catalogued 21+ findings of silent error
// swallowing at package boundaries.
package softfail

import (
	"log/slog"
	"maps"
	"strings"

	"github.com/nevinsm/sol/internal/events"
)

// Log records a best-effort error with context. It is intended for "soft"
// boundaries where the caller will continue with a fallback regardless of
// the error, but where silently discarding the error would mask real
// problems (permission denied, corrupted state, etc.).
//
// If logger is nil, [slog.Default] is used so callers without a plumbed
// logger still get observable failures. Returns true if err was non-nil
// (so callers can write `if softfail.Log(...) { /* fallback */ }`).
func Log(logger *slog.Logger, op string, err error) bool {
	if err == nil {
		return false
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("soft failure", "op", op, "error", err)
	return true
}

// EventEmitter is the minimal subset of an event logger that [Emit] needs to
// publish a structured soft-failure event. *events.Logger from
// internal/events satisfies this interface structurally.
//
// The interface is defined here (rather than imported) so test doubles can
// be supplied easily and so the package contract does not depend on the
// concrete *events.Logger type.
type EventEmitter interface {
	Emit(eventType, source, actor, visibility string, payload any)
}

// Emit logs the soft failure (like [Log]) and, when eventLogger is non-nil,
// also emits a structured "soft_failure" event so cross-domain consumers
// (chronicle, sol feed, audit) can observe the failure.
//
// op should be of the form "<source-component>.<short-action>", e.g.,
// "dispatch.rollback_agent_state". The portion before the first '.' is used
// as the event source and actor; if op contains no '.', the full op is used
// for both.
//
// payload is a small structured map of additional context for filters and
// ingest; nil is fine. The "op" and "error" keys are reserved — the helper
// always writes the canonical op string and err.Error() into them, even if
// the caller supplied other values.
//
// If logger is nil, [slog.Default] is used. If eventLogger is nil, the
// helper still logs the warning (matching [Log] semantics) but does not
// emit an event. Emission is synchronous, matching events.Logger.Emit.
//
// Returns true if err was non-nil, so callers can use the if-guard pattern:
//
//	if softfail.Emit(logger, evLogger, "dispatch.rollback", err, nil) {
//	    // fallback logic
//	}
func Emit(logger *slog.Logger, eventLogger EventEmitter, op string, err error, payload map[string]any) bool {
	if err == nil {
		return false
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("soft failure", "op", op, "error", err)
	if eventLogger == nil {
		return true
	}
	source := op
	if i := strings.IndexByte(op, '.'); i > 0 {
		source = op[:i]
	}
	finalPayload := make(map[string]any, len(payload)+2)
	maps.Copy(finalPayload, payload)
	finalPayload["op"] = op
	finalPayload["error"] = err.Error()
	eventLogger.Emit(events.EventSoftFailure, source, source, "audit", finalPayload)
	return true
}
