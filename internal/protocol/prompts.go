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

// OutpostSystemPrompt is the full-replace system prompt for the outpost agent role.
//
//go:embed prompts/outpost.md
var OutpostSystemPrompt string

// ForgeMergeSystemPrompt is the full-replace system prompt for forge merge sessions.
//
//go:embed prompts/forge-merge.md
var ForgeMergeSystemPrompt string

// ChancellorSystemPrompt is the append-mode system prompt for the chancellor role.
//
//go:embed prompts/chancellor.md
var ChancellorSystemPrompt string
