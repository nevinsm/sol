package worlds

import (
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/status"
)

// StatusResponse is the CLI API representation of world status --json output.
// It preserves the existing shape exactly.
type StatusResponse struct {
	*status.WorldStatus
	Config config.WorldConfig `json:"config"`
}
