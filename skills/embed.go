// Package skills embeds the distributable skill directories for inclusion
// in the sol binary. These skills are exported via `sol skill export`.
package skills

import "embed"

// FS contains the embedded skill directories (e.g., sol-integration/).
//
//go:embed all:sol-integration
var FS embed.FS
