package dispatch

import (
	"github.com/nevinsm/sol/internal/dispatch"
)

// ResolveResult is the CLI API representation of a resolve operation result.
//
// For code writs, all fields are populated. For non-code writs, only WritID,
// Agent, Kind, and Closed are set (Closed is true, branch/MR fields are empty).
type ResolveResult struct {
	WritID       string `json:"writ_id"`
	Agent        string `json:"agent"`
	Kind         string `json:"kind"`
	Branch       string `json:"branch,omitempty"`
	TargetBranch string `json:"target_branch,omitempty"`
	MRID         string `json:"mr_id,omitempty"`
	Closed       bool   `json:"closed,omitempty"`
}

// FromResolveResult converts a dispatch.ResolveResult to the CLI API type.
//
// kind is the writ kind (e.g. "code", "analysis"). targetBranch is the world's
// main branch (e.g. "main") — only relevant for code writs.
func FromResolveResult(r *dispatch.ResolveResult, kind, targetBranch string) ResolveResult {
	// Normalize empty kind to "code" (the default).
	if kind == "" {
		kind = "code"
	}

	isCode := kind == "code"

	res := ResolveResult{
		WritID: r.WritID,
		Agent:  r.AgentName,
		Kind:   kind,
	}

	if isCode {
		res.Branch = r.BranchName
		res.TargetBranch = targetBranch
		res.MRID = r.MergeRequestID
	} else {
		res.Closed = true
	}

	return res
}
