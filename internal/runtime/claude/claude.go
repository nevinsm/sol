// Package claude implements the Runtime interface for the Claude Code runtime.
// It is a port of internal/runtime/claude/ per ADR-0041: five behavioral
// methods (BuildCommand, WritePersona, InstallHooks, Seed, ExtractTelemetry)
// plus an embedded RuntimeDescriptor that carries all per-runtime configuration data.
package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/runtime"
	"github.com/nevinsm/sol/internal/runtime/attrutil"
)

// ClaudeRuntime implements runtime.Runtime for the Claude Code runtime.
type ClaudeRuntime struct {
	runtime.RuntimeDescriptor
}

// Compile-time interface satisfaction check.
var _ runtime.Runtime = (*ClaudeRuntime)(nil)

// New returns a new ClaudeRuntime with descriptor fields ported faithfully
// from internal/adapter/claude/claude.go.
func New() *ClaudeRuntime {
	return &ClaudeRuntime{
		RuntimeDescriptor: runtime.RuntimeDescriptor{
			Name:            "claude",
			PersonaFile:     "CLAUDE.local.md",
			SkillsDir:       ".claude/skills",
			ConfigDirEnv:    "CLAUDE_CONFIG_DIR",
			CredentialFile:  ".credentials.json",
			GlobalCredsPath: "~/.claude/.credentials.json",
			DefaultModel:    "sonnet",
			CalloutCommand:  "claude -p",
			// Claude Code supports all hook types natively as real runtime hooks.
			// The old adapter's SupportsHook returned true unconditionally, so all
			// known hook types are listed here.
			SupportedHooks: []string{"SessionStart", "PreCompact", "TurnBoundary", "Guard"},
			// CLAUDE_CODE_ENABLE_TELEMETRY is a runtime constant merged into telemetry env.
			StaticEnv: map[string]string{
				"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			},
			CredentialEnvKeys: map[string]string{
				"oauth_token": "CLAUDE_CODE_OAUTH_TOKEN",
				"api_key":     "ANTHROPIC_API_KEY",
			},
			// Claude Code supports the autoMemoryDirectory mechanism used by
			// sol for envoy persistent memory.
			SupportsAutoMemory: true,
		},
	}
}

// Descriptor returns the embedded RuntimeDescriptor.
func (r *ClaudeRuntime) Descriptor() runtime.RuntimeDescriptor {
	return r.RuntimeDescriptor
}

// BuildCommand constructs the claude session launch command string.
//
// Format:
//
//	claude --dangerously-skip-permissions [--continue] --settings <path>
//	    [--model <model>]
//	    [--system-prompt-file|--append-system-prompt-file <path>]
//	    [<prompt>]
//
// If SOL_SESSION_COMMAND is set (for testing), it is returned verbatim.
func (r *ClaudeRuntime) BuildCommand(ctx runtime.CommandContext) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}

	settingsPath := config.SettingsPath(ctx.WorktreeDir)

	args := "claude --dangerously-skip-permissions"

	if ctx.Continue {
		args += " --continue"
	}

	args += " --settings " + config.ShellQuote(settingsPath)

	if ctx.Model != "" {
		args += " --model " + ctx.Model
	}

	if ctx.SystemPromptFile != "" {
		if ctx.ReplacePrompt {
			args += " --system-prompt-file " + config.ShellQuote(ctx.SystemPromptFile)
		} else {
			args += " --append-system-prompt-file " + config.ShellQuote(ctx.SystemPromptFile)
		}
	}

	if ctx.Prompt != "" {
		args += " " + config.ShellQuote(ctx.Prompt)
	}

	return args
}

// hookEntry is a single hook handler in Claude Code's settings.local.json.
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookMatcherGroup is a matcher + its hook handlers.
type hookMatcherGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

// hookSettings is the top-level structure for Claude Code's settings.local.json.
// AutoMemoryDirectory is the absolute path to the per-agent MEMORY.md directory;
// emitted only when set (envoy role), so non-envoy roles get the same shape as
// before. Claude Code silently ignores relative paths here.
type hookSettings struct {
	AutoMemoryDirectory string                        `json:"autoMemoryDirectory,omitempty"`
	Hooks               map[string][]hookMatcherGroup `json:"hooks"`
}

// WritePersona writes the Claude persona file (CLAUDE.local.md) into the
// worktree root. Claude has no section model — the persona is a standalone
// file — so this just delegates to the shared file-writing helper.
func (r *ClaudeRuntime) WritePersona(ctx runtime.SpawnContext, content []byte) error {
	return runtime.WritePersonaFile(r.RuntimeDescriptor, ctx.WorktreeDir, content)
}

// InstallHooks translates the runtime-agnostic HookSet to Claude Code hook JSON
// and writes it to {worktreeDir}/.claude/settings.local.json. For envoy
// sessions, also writes autoMemoryDirectory pointing at the per-agent memory
// dir so Claude Code's native auto-memory mechanism finds the agent's MEMORY.md
// on session start.
//
// Mapping:
//   - HookSet.SessionStart  → Claude Code "SessionStart" hook entries
//   - HookSet.PreCompact    → Claude Code "PreCompact" hook entries
//   - HookSet.Guards        → Claude Code "PreToolUse" hook entries
//   - HookSet.TurnBoundary  → Claude Code "UserPromptSubmit" hook entries
func (r *ClaudeRuntime) InstallHooks(ctx runtime.SpawnContext, hooks runtime.HookSet) error {
	worktreeDir := ctx.WorktreeDir
	hooksMap := map[string][]hookMatcherGroup{}

	// SessionStart
	if len(hooks.SessionStart) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.SessionStart))
		for _, hc := range hooks.SessionStart {
			groups = append(groups, hookMatcherGroup{
				Matcher: hc.Matcher,
				Hooks:   []hookEntry{{Type: "command", Command: hc.Command}},
			})
		}
		hooksMap["SessionStart"] = groups
	}

	// PreCompact
	if len(hooks.PreCompact) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.PreCompact))
		for _, hc := range hooks.PreCompact {
			groups = append(groups, hookMatcherGroup{
				Matcher: hc.Matcher,
				Hooks:   []hookEntry{{Type: "command", Command: hc.Command}},
			})
		}
		hooksMap["PreCompact"] = groups
	}

	// PreToolUse (Guards)
	if len(hooks.Guards) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.Guards))
		for _, g := range hooks.Guards {
			groups = append(groups, hookMatcherGroup{
				Matcher: g.Pattern,
				Hooks:   []hookEntry{{Type: "command", Command: g.Command}},
			})
		}
		hooksMap["PreToolUse"] = groups
	}

	// UserPromptSubmit (TurnBoundary)
	if len(hooks.TurnBoundary) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.TurnBoundary))
		for _, hc := range hooks.TurnBoundary {
			groups = append(groups, hookMatcherGroup{
				Matcher: hc.Matcher,
				Hooks:   []hookEntry{{Type: "command", Command: hc.Command}},
			})
		}
		hooksMap["UserPromptSubmit"] = groups
	}

	settings := hookSettings{
		Hooks: hooksMap,
	}

	// Envoys get per-agent persistent memory wired up via Claude Code's
	// autoMemoryDirectory. Outposts and forge-merge are ephemeral / per-writ
	// and intentionally have no persistent memory. The path must be absolute;
	// Claude Code silently ignores relative autoMemoryDirectory values.
	if ctx.Role == "envoy" {
		memoryDir := runtime.MemoryDir(ctx.WorldDir, ctx.Role, ctx.Agent)
		if memoryDir != "" && !filepath.IsAbs(memoryDir) {
			return fmt.Errorf("claude runtime: MemoryDir returned relative path %q for role=%q agent=%q", memoryDir, ctx.Role, ctx.Agent)
		}
		settings.AutoMemoryDirectory = memoryDir
	}

	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("claude runtime: failed to create .claude directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("claude runtime: failed to marshal hook settings: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := fileutil.AtomicWrite(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("claude runtime: failed to write settings.local.json: %w", err)
	}

	return nil
}

// Seed populates Claude-specific state into the per-agent config dir:
//   - settings.json + plugins + onboarding markers (SeedClaudeConfig)
//   - hasTrustDialogAccepted entry for the worktree in .claude.json
//     (otherwise Claude Code prompts "Do you trust this directory?" on first run)
//   - per-agent memory directory for envoys (so the autoMemoryDirectory
//     setting written by InstallHooks resolves to an existing path)
//
// Without these, fresh outpost spawns block at one of Claude Code's first-run
// prompts (welcome wizard, trust dialog).
func (r *ClaudeRuntime) Seed(ctx runtime.SpawnContext) error {
	if err := config.SeedClaudeConfig(ctx.ConfigDir); err != nil {
		return err
	}
	if ctx.WorktreeDir != "" {
		if err := protocol.TrustDirectoryIn(ctx.WorktreeDir, ctx.ConfigDir); err != nil {
			return fmt.Errorf("claude runtime: failed to pre-trust worktree %q in config dir %q: %w", ctx.WorktreeDir, ctx.ConfigDir, err)
		}
	}
	if ctx.Role == "envoy" {
		memoryDir := runtime.MemoryDir(ctx.WorldDir, ctx.Role, ctx.Agent)
		if memoryDir != "" {
			if err := os.MkdirAll(memoryDir, 0o755); err != nil {
				return fmt.Errorf("claude runtime: failed to create memory dir %q: %w", memoryDir, err)
			}
		}
	}
	return nil
}

// ExtractTelemetry extracts token usage data from a Claude Code log event.
// Accepts events named "claude_code.api_request" or "api_request".
// Returns nil if the event is not relevant or has no model information.
func (r *ClaudeRuntime) ExtractTelemetry(eventName string, attrs map[string]string) *runtime.TelemetryRecord {
	if eventName != "claude_code.api_request" && eventName != "api_request" {
		return nil
	}

	// Extract model — short name first, then gen_ai.* fallback.
	model := attrs["model"]
	if model == "" {
		model = attrs["gen_ai.response.model"]
	}
	if model == "" {
		return nil
	}

	input := attrutil.ParseInt(attrs, "input_tokens")
	if input == 0 {
		input = attrutil.ParseInt(attrs, "gen_ai.usage.input_tokens")
	}
	output := attrutil.ParseInt(attrs, "output_tokens")
	if output == 0 {
		output = attrutil.ParseInt(attrs, "gen_ai.usage.output_tokens")
	}
	cacheRead := attrutil.ParseInt(attrs, "cache_read_tokens")
	if cacheRead == 0 {
		cacheRead = attrutil.ParseInt(attrs, "gen_ai.usage.cache_read_input_tokens")
	}
	cacheCreation := attrutil.ParseInt(attrs, "cache_creation_tokens")
	if cacheCreation == 0 {
		cacheCreation = attrutil.ParseInt(attrs, "gen_ai.usage.cache_creation_input_tokens")
	}
	// Reasoning tokens are emitted by Claude Code when extended thinking is
	// enabled. Try several known and likely keys (short name then gen_ai.* fallback).
	reasoning := attrutil.ParseInt(attrs, "reasoning_tokens")
	if reasoning == 0 {
		reasoning = attrutil.ParseInt(attrs, "reasoning_token_count")
	}
	if reasoning == 0 {
		reasoning = attrutil.ParseInt(attrs, "gen_ai.usage.reasoning_tokens")
	}

	costUSD := attrutil.ParseFloat(attrs, "cost_usd")
	durationMS := attrutil.ParseIntPtr(attrs, "duration_ms")

	return &runtime.TelemetryRecord{
		Model:               model,
		InputTokens:         input,
		OutputTokens:        output,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreation,
		ReasoningTokens:     reasoning,
		CostUSD:             costUSD,
		DurationMS:          durationMS,
	}
}
