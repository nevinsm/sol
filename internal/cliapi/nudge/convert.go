package nudge

import (
	"fmt"

	internalnudge "github.com/nevinsm/sol/internal/nudge"
)

// FromMessage converts an internal nudge.Message to the CLI API Nudge type.
// The target parameter is the session name (agent target) for this nudge.
// The ID is derived from the message's CreatedAt timestamp (unix milliseconds),
// matching the file-naming convention used by the nudge queue.
func FromMessage(msg internalnudge.Message, target string) Nudge {
	return Nudge{
		ID:       fmt.Sprintf("%d", msg.CreatedAt.UnixMilli()),
		Target:   target,
		Body:     msg.Body,
		Source:   msg.Sender,
		QueuedAt: msg.CreatedAt,
	}
}

// FromMessages converts a slice of internal nudge.Message to CLI API Nudge types.
// Returns an empty (non-nil) slice when the input is empty, per cliapi conventions.
func FromMessages(msgs []internalnudge.Message, target string) []Nudge {
	out := make([]Nudge, len(msgs))
	for i, m := range msgs {
		out[i] = FromMessage(m, target)
	}
	return out
}
