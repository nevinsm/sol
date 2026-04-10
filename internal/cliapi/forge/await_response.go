package forge

import "github.com/nevinsm/sol/internal/nudge"

// ForgeAwaitResponse is the CLI API representation of `forge await` output.
type ForgeAwaitResponse struct {
	Woke          bool            `json:"woke"`
	Messages      []nudge.Message `json:"messages"`
	WaitedSeconds float64         `json:"waited_seconds"`
}
