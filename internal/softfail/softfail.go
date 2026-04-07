// Package softfail provides a tiny helper for "log + continue" error sites.
//
// Many sol code paths intentionally treat errors as soft failures: a missing
// optional file, an unexpected on-disk shape, a best-effort discovery scan.
// The correct behavior is to keep going with a sensible fallback, but the
// implementation must not silently discard the error — that turns recoverable
// problems (permission denied, corrupted state) into silent misbehavior.
//
// Use [Log] at those boundaries instead of `_ = err` or a bare `return`.
//
// See pattern P1 ("error swallowing in error paths") in the failure-mode
// catalog and writ sol-18364486adf34c8d for context.
package softfail

import "log/slog"

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
