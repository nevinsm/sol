package defaults

import _ "embed"

// StatuslineSh is the default statusline script for Claude Code sessions.
//
//go:embed statusline.sh
var StatuslineSh []byte

// SettingsJSON is the template for the default settings.json.
// The placeholder {{STATUSLINE_PATH}} must be replaced with the absolute
// path to the statusline.sh script before writing to disk.
//
//go:embed settings.json
var SettingsJSON []byte
