package dispatch

import (
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/startup"
)

// OutpostResumeState builds a startup.ResumeState for outpost compact recovery.
// Reads the current workflow step and tethered work item to determine where
// the agent should resume from.
func OutpostResumeState(world, agent string) startup.ResumeState {
	return handoff.CaptureResumeState(world, agent, "agent", "compact")
}
