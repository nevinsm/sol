package protocol

import _ "embed"

// GovernorSystemPrompt is the append-mode system prompt for the governor role.
//
//go:embed prompts/governor.md
var GovernorSystemPrompt string

// EnvoySystemPrompt is the append-mode system prompt for the envoy role.
//
//go:embed prompts/envoy.md
var EnvoySystemPrompt string

// ForgeSystemPrompt is the full-replacement system prompt for the forge role.
//
//go:embed prompts/forge.md
var ForgeSystemPrompt string

// OutpostSystemPrompt is the full-replace system prompt for the outpost agent role.
//
//go:embed prompts/outpost.md
var OutpostSystemPrompt string
