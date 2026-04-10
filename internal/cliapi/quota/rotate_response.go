package quota

import iquota "github.com/nevinsm/sol/internal/quota"

// RotateAction describes a single credential rotation in `sol quota rotate --json`.
type RotateAction struct {
	Agent       string `json:"agent"`
	FromAccount string `json:"from_account"`
	ToAccount   string `json:"to_account,omitempty"`
	Paused      bool   `json:"paused,omitempty"`
}

// RotateResponse is the top-level JSON output for `sol quota rotate --json`.
type RotateResponse struct {
	Actions []RotateAction `json:"actions"`
	Expired []string       `json:"expired"`
	DryRun  bool           `json:"dry_run"`
}

// NewRotateAction converts an internal RotationAction to the CLI API RotateAction type.
func NewRotateAction(a iquota.RotationAction) RotateAction {
	return RotateAction{
		Agent:       a.AgentName,
		FromAccount: a.FromAccount,
		ToAccount:   a.ToAccount,
		Paused:      a.Paused,
	}
}

// NewRotateResponse converts an internal RotateResult to the CLI API RotateResponse.
func NewRotateResponse(result *iquota.RotateResult, dryRun bool) RotateResponse {
	actions := make([]RotateAction, 0, len(result.Actions))
	for _, a := range result.Actions {
		actions = append(actions, NewRotateAction(a))
	}
	expired := result.Expired
	if expired == nil {
		expired = []string{}
	}
	return RotateResponse{
		Actions: actions,
		Expired: expired,
		DryRun:  dryRun,
	}
}
