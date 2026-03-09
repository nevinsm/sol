package startup

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	UpdateAgentState(id, state, activeWrit string) error
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
	Workflow  string // workflow name to instantiate (empty = none)
	NeedsItem bool   // whether workflow requires a writ

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

	// SessionOp, when set, replaces the default Sessions.Start() call in step 9
	// of Launch. Used by handoff for atomic session cycling via respawn-pane.
	// Signature matches SessionStarter.Start.
	SessionOp func(name, workdir, cmd string, env map[string]string, role, world string) error
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
//  6. Instantiate workflow if cfg.Workflow is set
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

	// 4.5. Pre-trust the working directory in the agent's config dir so
	// Claude Code doesn't block on an interactive trust prompt when using
	// the agent-specific CLAUDE_CONFIG_DIR.
	if err := protocol.TrustDirectoryIn(worktreeDir, claudeConfigDir); err != nil {
		fmt.Fprintf(os.Stderr, "startup: failed to pre-trust directory in config dir %s: %v\n", claudeConfigDir, err)
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
	existing, getErr := sphereStore.GetAgent(agentID)
	if getErr != nil {
		if _, err := sphereStore.CreateAgent(agent, world, cfg.Role); err != nil {
			return "", fmt.Errorf("startup: failed to register agent: %w", err)
		}
	}
	// Preserve existing tether item (outpost agents have tethered writs).
	activeWrit := ""
	if existing != nil {
		activeWrit = existing.ActiveWrit
	}
	if err := sphereStore.UpdateAgentState(agentID, "working", activeWrit); err != nil {
		return "", fmt.Errorf("startup: failed to set agent working: %w", err)
	}

	// 6. Instantiate workflow if set.
	if cfg.Workflow != "" {
		// Instantiate if no workflow exists or previous one completed.
		// A done workflow has no useful state to preserve — re-instantiate
		// so looping workflows (e.g. forge-patrol) restart from step 1.
		existingState, _ := workflow.ReadState(world, agent, cfg.Role)
		if existingState == nil || existingState.Status == "done" {
			vars := map[string]string{
				"world": world,
			}
			if _, _, err := workflow.Instantiate(world, agent, cfg.Role, cfg.Workflow, vars); err != nil {
				return "", fmt.Errorf("startup: failed to instantiate workflow %q: %w", cfg.Workflow, err)
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

	// Enable ledger telemetry if configured.
	if worldCfg.Ledger.Port > 0 {
		env["CLAUDE_CODE_ENABLE_TELEMETRY"] = "1"
		env["OTEL_LOGS_EXPORTER"] = "otlp"
		env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] = fmt.Sprintf("http://localhost:%d", worldCfg.Ledger.Port)
		env["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"] = "http/json"
		attrs := fmt.Sprintf("agent.name=%s,world=%s", agent, world)
		if activeWrit != "" {
			attrs += ",writ_id=" + activeWrit
		}
		env["OTEL_RESOURCE_ATTRIBUTES"] = attrs
	}

	// 9. Start (or cycle) the tmux session.
	if opts.SessionOp != nil {
		if err := opts.SessionOp(sessName, worktreeDir, sessionCmd, env, cfg.Role, world); err != nil {
			return "", fmt.Errorf("startup: session operation failed: %w", err)
		}
	} else {
		mgr := resolveSessionStarter(opts)
		if err := mgr.Start(sessName, worktreeDir, sessionCmd, env, cfg.Role, world); err != nil {
			return "", fmt.Errorf("startup: failed to start session: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "startup: session %s started (role=%s, world=%s)\n", sessName, cfg.Role, world)

	return sessName, nil
}

// ResumeState captures workflow state for compact handoff recovery.
type ResumeState struct {
	CurrentStep     string // workflow step ID the agent was on
	StepDescription string // human-readable step title
	ClaimedResource string // MR ID or work-in-progress identifier
	Reason          string // why handoff happened: "compact", "manual", "error", "writ-switch"

	// Writ-switch fields (populated when Reason == "writ-switch").
	PreviousActiveWrit string // writ ID that was active before the switch
	NewActiveWrit      string // writ ID that is now active
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
		return BuildResumePrime(base, state)
	}

	return Launch(cfg, world, agent, opts)
}

// Respawn looks up the registered config for a role and performs a Launch
// (or Resume if a resume-state file exists). It encapsulates the
// read-resume → Resume-or-Launch decision so callers don't duplicate it.
func Respawn(role, world, agent string, opts LaunchOpts) (string, error) {
	cfg := ConfigFor(role)
	if cfg == nil {
		return "", fmt.Errorf("no startup config registered for role %q", role)
	}

	opts.Respawn = true

	resumeState, _ := ReadResumeState(world, agent, role)
	if resumeState != nil {
		slog.Info("found resume state, using startup.Resume",
			"agent", agent, "world", world, "reason", resumeState.Reason)
		sessName, err := Resume(*cfg, world, agent, *resumeState, opts)
		// Clear the file whether Resume succeeded or not — stale state
		// is worse than no state on the next attempt.
		ClearResumeState(world, agent, role)
		return sessName, err
	}

	return Launch(*cfg, world, agent, opts)
}

// BuildResumePrime constructs a resume-aware prime prompt that includes
// workflow state and claimed resource information.
func BuildResumePrime(base string, state ResumeState) string {
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

	if state.Reason == "writ-switch" {
		if state.PreviousActiveWrit != "" && state.NewActiveWrit != "" {
			fmt.Fprintf(&b, "Your active writ has changed to %s. Previous active was %s.\n", state.NewActiveWrit, state.PreviousActiveWrit)
		} else if state.NewActiveWrit != "" {
			fmt.Fprintf(&b, "Your active writ has changed to %s.\n", state.NewActiveWrit)
		}
	} else if state.NewActiveWrit != "" {
		// Non-writ-switch resume with an active writ: restore active writ context.
		fmt.Fprintf(&b, "Active writ: %s\n", state.NewActiveWrit)
	}

	if base != "" {
		b.WriteString("\n")
		b.WriteString(base)
	}

	return b.String()
}

// resumeStateFile returns the path to the resume state file for an agent.
const resumeStateFilename = ".resume_state.json"

func resumeStatePath(world, agent, role string) string {
	return filepath.Join(config.AgentDir(world, agent, role), resumeStateFilename)
}

// WriteResumeState persists a ResumeState to disk so a subsequent respawn
// can recover workflow position via Resume() instead of a fresh Launch().
func WriteResumeState(world, agent, role string, state ResumeState) error {
	p := resumeStatePath(world, agent, role)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("startup: failed to create dir for resume state: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("startup: failed to marshal resume state: %w", err)
	}
	return os.WriteFile(p, data, 0o644)
}

// ReadResumeState loads a previously written ResumeState from disk.
// Returns nil, nil if no file exists.
func ReadResumeState(world, agent, role string) (*ResumeState, error) {
	data, err := os.ReadFile(resumeStatePath(world, agent, role))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("startup: failed to read resume state: %w", err)
	}
	var state ResumeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("startup: failed to unmarshal resume state: %w", err)
	}
	return &state, nil
}

// ClearResumeState removes the resume state file after it has been consumed.
func ClearResumeState(world, agent, role string) error {
	err := os.Remove(resumeStatePath(world, agent, role))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("startup: failed to clear resume state: %w", err)
	}
	return nil
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

// installPersona writes persona content to CLAUDE.local.md at the worktree root.
// Written at root level so Claude Code's upward directory walk discovers it.
// Skills are installed separately by the Install*ClaudeMD functions or Launch.
func installPersona(worktreeDir string, content []byte) error {
	path := filepath.Join(worktreeDir, "CLAUDE.local.md")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.local.md: %w", err)
	}
	return nil
}
