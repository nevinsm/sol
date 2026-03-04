// Package docs provides embedded documentation assets.
package docs

import _ "embed"

// CLIReference holds the contents of cli.md, embedded at build time.
//
//go:embed cli.md
var CLIReference string
