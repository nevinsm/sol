package protocol

import _ "embed"

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

