package defaults

import _ "embed"

// StatuslineSh is the default statusline script for Claude Code sessions.
//
//go:embed statusline.sh
var StatuslineSh []byte

// ApiKeyHelperSh is the default API key helper script for Claude Code sessions.
// Called periodically by Claude Code's apiKeyHelper mechanism to return a fresh
// OAuth access token from broker-managed credentials.
//
//go:embed apikey-helper.sh
var ApiKeyHelperSh []byte

// SettingsJSON is the template for the default settings.json.
// Placeholders that must be replaced before writing to disk:
//   - {{STATUSLINE_PATH}} — absolute path to statusline.sh
//   - {{API_KEY_HELPER_PATH}} — absolute path to apikey-helper.sh
//
//go:embed settings.json
var SettingsJSON []byte
