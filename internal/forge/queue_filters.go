package forge

import (
	"sort"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// DefaultQueueStatuses is the active-status filter applied by `sol forge
// queue` when neither --all nor --status is provided. Merged MRs are
// intentionally excluded — use `sol forge history` to browse historical
// merges.
var DefaultQueueStatuses = []string{store.MRReady, store.MRClaimed, store.MRFailed}

// FilterQueueByStatus applies the `sol forge queue` status filter semantics:
//
//   - If all=true, return mrs unchanged.
//   - Else, if status is non-empty, parse it as a comma-separated list and
//     keep MRs whose Phase is in the set.
//   - Else, keep MRs whose Phase is in DefaultQueueStatuses.
func FilterQueueByStatus(mrs []store.MergeRequest, all bool, status string) []store.MergeRequest {
	if all {
		return mrs
	}
	set := map[string]bool{}
	if status != "" {
		for s := range strings.SplitSeq(status, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				set[s] = true
			}
		}
	} else {
		for _, s := range DefaultQueueStatuses {
			set[s] = true
		}
	}
	filtered := mrs[:0:0]
	for _, mr := range mrs {
		if set[mr.Phase] {
			filtered = append(filtered, mr)
		}
	}
	return filtered
}

// FilterHistory sorts newest-first, applies since/until bounds on the history
// timestamp (MergedAt fallback UpdatedAt), and trims to limit. limit<=0 means
// no limit.
func FilterHistory(mrs []store.MergeRequest, since, until *time.Time, limit int) []store.MergeRequest {
	sorted := append([]store.MergeRequest(nil), mrs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return HistoryTimestamp(sorted[i]).After(HistoryTimestamp(sorted[j]))
	})
	if since != nil || until != nil {
		filtered := sorted[:0:0]
		for _, mr := range sorted {
			ts := HistoryTimestamp(mr)
			if since != nil && ts.Before(*since) {
				continue
			}
			if until != nil && ts.After(*until) {
				continue
			}
			filtered = append(filtered, mr)
		}
		sorted = filtered
	}
	if limit > 0 && len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// HistoryTimestamp returns the best-available "when did this merge land"
// timestamp for an MR: MergedAt if set, otherwise UpdatedAt.
func HistoryTimestamp(mr store.MergeRequest) time.Time {
	if mr.MergedAt != nil {
		return *mr.MergedAt
	}
	return mr.UpdatedAt
}
