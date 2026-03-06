package startup

import (
	"fmt"
	"os"
	"strings"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/workflow"
)

// HookSet holds all the Claude Code hooks for a role.
type HookSet = protocol.HookConfig

// SessionStarter abstracts tmux session creation for testing.
type SessionStarter interface {
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
}

// SphereStore abstracts sphere database operations for testing.
type SphereStore interface {
	GetAgent(id string) (*store.Agent, error)
	CreateAgent(name, world, role string) (string, error)
	UpdateAgentState(id, state, tetherItem string) error
	Close() error
}

// RoleConfig describes the startup configuration for a role.
type RoleConfig struct {
	// Identity
	Role string

	// Paths
	WorktreeDir func(world, agent string) string

	// Persona & Hooks
	Persona func(world, agent string) ([]byte, error) // CLAUDE.local.md content
	Hooks   func(world, agent string) HookSet

	// System prompt
	SystemPromptFile    string // path to prompt file (embedded or on disk)
	SystemPromptContent string // if set, written to .claude/system-prompt.md and used as SystemPromptFile
	ReplacePrompt       bool   // true = --system-prompt-file, false = --append-system-prompt-file

	// Workflow
	Formula   string // formula name to instantiate (empty = none)
	NeedsItem bool   // whether formula requires a work item

	// Prime context
	PrimeBuilder func(world, agent string) string
}

// LaunchOpts holds optional parameters for Launch.
type LaunchOpts struct {
	Continue bool   // use --continue for handoff
	Respawn  bool   // skip worktree creation if exists
	Account  string // account override (empty = use world default)

	// Optional dependency injection for testing. When nil, defaults are used.
	Sessions   SessionStarter // default: session.New()
	Sphere     SphereStore    // default: store.OpenSphere()
	OwnsSphere bool           // if true, Launch closes the sphere store on exit
}

// registry maps role names to their RoleConfig.
var registry = map[string]*RoleConfig{}

// Register adds a role configuration to the registry.
func Register(role string, cfg RoleConfig) {
	cfg.Role = role
	registry[role] = &cfg
}

// ConfigFor returns the registered RoleConfig for a role.
// Returns nil if no config is registered.
func ConfigFor(role string) *RoleConfig {
	return registry[role]
}

// Launch executes the universal agent session launch sequence.
// Steps:
//  1. Ensure worktree exists
//  2. Install persona (cfg.Persona → CLAUDE.local.md)
//  3. Install hooks (cfg.Hooks → settings.local.json)
//  4. Ensure CLAUDE_CONFIG_DIR (config.EnsureClaudeConfigDir)
//  5. Ensure agent record in sphere store
//  6. Instantiate workflow if cfg.Formula is set
//  7. Build prime context (cfg.PrimeBuilder)
//  8. Build claude command (--system-prompt-file or --append-system-prompt-file)
//  9. Start tmux session with env
func Launch(cfg RoleConfig, world, agent string, opts LaunchOpts) (string, error) {
	sessName := config.SessionName(world, agent)

	// 1. Get worktree directory.
	worktreeDir := ""
	if cfg.WorktreeDir != nil {
		worktreeDir = cfg.WorktreeDir(world, agent)
	}
	if worktreeDir == "" {
		return "", fmt.Errorf("startup: worktree dir is required for role %q", cfg.Role)
	}

	// Verify worktree exists (callers are responsible for creating it).
	if info, err := os.Stat(worktreeDir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("startup: worktree directory does not exist: %s", worktreeDir)
	}

	// 2. Install persona (CLAUDE.local.md).
	if cfg.Persona != nil {
		content, err := cfg.Persona(world, agent)
		if err != nil {
			return "", fmt.Errorf("startup: failed to generate persona: %w", err)
		}
		if err := installPersona(worktreeDir, content); err != nil {
			return "", fmt.Errorf("startup: failed to install persona: %w", err)
		}
	}

	// 2.5. Install system prompt content if provided.
	if cfg.SystemPromptContent != "" {
		promptDir := fmt.Sprintf("%s/.claude", worktreeDir)
		if err := os.MkdirAll(promptDir, 0o755); err != nil {
			return "", fmt.Errorf("startup: failed to create .claude for system prompt: %w", err)
		}
		promptPath := fmt.Sprintf("%s/system-prompt.md", promptDir)
		if err := os.WriteFile(promptPath, []byte(cfg.SystemPromptContent), 0o644); err != nil {
			return "", fmt.Errorf("startup: failed to write system prompt: %w", err)
		}
		cfg.SystemPromptFile = ".claude/system-prompt.md"
	}

	// 3. Install hooks (settings.local.json).
	if cfg.Hooks != nil {
		hookCfg := cfg.Hooks(world, agent)
		if err := protocol.WriteHookSettings(worktreeDir, hookCfg); err != nil {
			return "", fmt.Errorf("startup: failed to install hooks: %w", err)
		}
	}

	// 4. Ensure CLAUDE_CONFIG_DIR.
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		return "", fmt.Errorf("startup: failed to load world config: %w", err)
	}
	resolvedAccount := opts.Account
	if resolvedAccount == "" {
		resolvedAccount = account.ResolveAccount("", worldCfg.World.DefaultAccount)
	}
	claudeConfigDir, err := config.EnsureClaudeConfigDir(config.WorldDir(world), cfg.Role, agent, resolvedAccount)
	if err != nil {
		return "", fmt.Errorf("startup: failed to ensure claude config dir: %w", err)
	}

	// 5. Ensure agent record in sphere store.
	sphereStore, closeSphere, err := resolveSphereStore(opts)
	if err != nil {
		return "", fmt.Errorf("startup: failed to open sphere store: %w", err)
	}
	if closeSphere != nil {
		defer closeSphere()
	}

	agentID := world + "/" + agent
	if _, err := sphereStore.GetAgent(agentID); err != nil {
		if _, err := sphereStore.CreateAgent(agent, world, cfg.Role); err != nil {
			return "", fmt.Errorf("startup: failed to register agent: %w", err)
		}
	}
	if err := sphereStore.UpdateAgentState(agentID, "working", ""); err != nil {
		return "", fmt.Errorf("startup: failed to set agent working: %w", err)
	}

	// 6. Instantiate workflow if formula is set.
	if cfg.Formula != "" {
		// Only instantiate if no workflow is already active.
		existingState, _ := workflow.ReadState(world, agent, cfg.Role)
		if existingState == nil {
			vars := map[string]string{
				"world": world,
			}
			if _, _, err := workflow.Instantiate(world, agent, cfg.Role, cfg.Formula, vars); err != nil {
				return "", fmt.Errorf("startup: failed to instantiate formula %q: %w", cfg.Formula, err)
			}
		}
	}

	// 7. Build prime context.
	prompt := ""
	if cfg.PrimeBuilder != nil {
		prompt = cfg.PrimeBuilder(world, agent)
	}

	// 8. Build claude command.
	sessionCmd := buildCommand(cfg, worktreeDir, prompt, opts.Continue)

	// 9. Start tmux session.
	env := map[string]string{
		"SOL_HOME":          config.Home(),
		"SOL_WORLD":         world,
		"SOL_AGENT":         agent,
		"CLAUDE_CONFIG_DIR": claudeConfigDir,
	}

	mgr := resolveSessionStarter(opts)
	if err := mgr.Start(sessName, worktreeDir, sessionCmd, env, cfg.Role, world); err != nil {
		return "", fmt.Errorf("startup: failed to start session: %w", err)
	}

	return sessName, nil
}

// ResumeState captures workflow state for compact handoff recovery.
type ResumeState struct {
	CurrentStep     string // workflow step ID the agent was on
	StepDescription string // human-readable step title
	ClaimedResource string // MR ID or work-in-progress identifier
	Reason          string // why handoff happened: "compact", "manual", "error"
}

// Resume does everything Launch does but uses --continue for conversation
// continuity and injects workflow state into the prime context.
//
// The resume prime prepends state context ("You were on step N...") to the
// role's normal prime, so the agent immediately knows where it left off.
func Resume(cfg RoleConfig, world, agent string, state ResumeState, opts LaunchOpts) (string, error) {
	opts.Continue = true

	origPrime := cfg.PrimeBuilder
	cfg.PrimeBuilder = func(w, a string) string {
		base := ""
		if origPrime != nil {
			base = origPrime(w, a)
		}
		return buildResumePrime(base, state)
	}

	return Launch(cfg, world, agent, opts)
}

// buildResumePrime constructs a resume-aware prime prompt that includes
// workflow state and claimed resource information.
func buildResumePrime(base string, state ResumeState) string {
	var b strings.Builder
	b.WriteString("[RESUME] Session recovery")
	if state.Reason != "" {
		fmt.Fprintf(&b, " (reason: %s)", state.Reason)
	}
	b.WriteString(".\n")

	if state.CurrentStep != "" {
		if state.StepDescription != "" {
			fmt.Fprintf(&b, "You were on step %s (%s). Resume from there.\n", state.CurrentStep, state.StepDescription)
		} else {
			fmt.Fprintf(&b, "You were on step %s. Resume from there.\n", state.CurrentStep)
		}
	}

	if state.ClaimedResource != "" {
		fmt.Fprintf(&b, "Claimed resource: %s is claimed and in-progress.\n", state.ClaimedResource)
	}

	if base != "" {
		b.WriteString("\n")
		b.WriteString(base)
	}

	return b.String()
}

// resolveSphereStore returns the sphere store and an optional cleanup function.
func resolveSphereStore(opts LaunchOpts) (SphereStore, func(), error) {
	if opts.Sphere != nil {
		return opts.Sphere, nil, nil
	}
	s, err := store.OpenSphere()
	if err != nil {
		return nil, nil, err
	}
	return s, func() { s.Close() }, nil
}

// resolveSessionStarter returns the session starter from opts or a default.
func resolveSessionStarter(opts LaunchOpts) SessionStarter {
	if opts.Sessions != nil {
		return opts.Sessions
	}
	return session.New()
}

// buildCommand constructs the claude startup command with system prompt flags.
func buildCommand(cfg RoleConfig, worktreeDir, prompt string, continueSession bool) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}

	settingsPath := config.SettingsPath(worktreeDir)

	args := "claude --dangerously-skip-permissions"

	if continueSession {
		args += " --continue"
	}

	args += " --settings " + config.ShellQuote(settingsPath)

	if cfg.SystemPromptFile != "" {
		if cfg.ReplacePrompt {
			args += " --system-prompt-file " + config.ShellQuote(cfg.SystemPromptFile)
		} else {
			args += " --append-system-prompt-file " + config.ShellQuote(cfg.SystemPromptFile)
		}
	}

	if prompt != "" {
		args += " " + config.ShellQuote(prompt)
	}

	return args
}

// installPersona writes persona content to .claude/CLAUDE.local.md.
func installPersona(worktreeDir string, content []byte) error {
	claudeDir := fmt.Sprintf("%s/.claude", worktreeDir)
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	path := fmt.Sprintf("%s/CLAUDE.local.md", claudeDir)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.local.md: %w", err)
	}

	return protocol.InstallCLIReference(worktreeDir)
}
