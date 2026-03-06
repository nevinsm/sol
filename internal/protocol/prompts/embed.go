package prompts

import _ "embed"

// OutpostSystemPrompt holds the outpost system prompt, embedded at build time.
//
//go:embed outpost.md
var OutpostSystemPrompt string
