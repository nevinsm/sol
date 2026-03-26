package persona

import "embed"

//go:embed defaults/planner.md
//go:embed defaults/engineer.md
var defaultPersonas embed.FS

// knownDefaults lists persona template names that are embedded in the binary.
var knownDefaults = map[string]bool{
	"planner":  true,
	"engineer": true,
}
