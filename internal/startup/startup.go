package startup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/adapter"
	_ "github.com/nevinsm/sol/internal/adapter/claude" // register the "claude" runtime adapter
	_ "github.com/nevinsm/sol/internal/adapter/codex"  // register the "codex" runtime adapter
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envfile"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
)

// HookSet is the runtime-agnostic hook configuration for a role session.
// It is an alias for adapter.HookSet.
type HookSet = adapter.HookSet

// HookCommand is an alias for adapter.HookCommand (for use in role packages).
type HookCommand = adapter.HookCommand

// Guard is an alias for adapter.Guard (for use in role packages).
type Guard = adapter.Guard

// SessionStarter abstracts tmux session creation for testing.
type SessionStarter interface {
	Exists(name string) bool
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
	SystemPromptContent string                         // if set, written via adapter.InjectSystemPrompt
	ReplacePrompt       bool                           // true = --system-prompt-file, false = --append-system-prompt-file
	PersonaFile         func(world, agent string) string // returns path to persona file (or empty); content appended to system prompt

	// Skills
	SkillInstaller func(world, agent string) []adapter.Skill // builds skills (adapter writes them)

	// Prime context
	PrimeBuilder func(world, agent string) string

	// Runtime adapter (resolved from world config at launch time if nil)
	Adapter adapter.RuntimeAdapter

	// WorldConfigHook, if set, is called by Launch after loading the
	// WorldConfig. Persona and SkillInstaller callbacks can use a shared
	// variable populated by this hook instead of reloading config
	// independently.
	WorldConfigHook func(*config.WorldConfig)
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

	// WorldConfig, when non-nil, provides a pre-loaded world config.
	// Skips the independent config.LoadWorldConfig call in Launch.
	// Used by dispatch.Cast to avoid loading the config twice.
	WorldConfig *config.WorldConfig

	// SessionOp, when set, replaces the default Sessions.Start() call in step 14
	// of Launch. Used by handoff for atomic session cycling via respawn-pane.
	// Signature matches SessionStarter.Start.
	SessionOp func(name, workdir, cmd string, env map[string]string, role, world string) error

	// WritExists, when set, is forwarded into BuildResumePrime by Resume so
	// the resume prime can downgrade phantom-writ references (CD-6). Callers
	// that have a world store handy (dispatch.ActivateWrit, prefect respawn)
	// should supply a closure that reads the world store; callers without
	// one (low-level tests, integration paths that don't care) can leave it
	// nil and the resume prime renders unchanged.
	WritExists WritExistsFunc
}

// registry maps role names to their RoleConfig.
var registry = map[string]*RoleConfig{}

// Register adds a role configuration to the registry.
func Register(role string, cfg RoleConfig) {
	cfg.Role = role
	registry[role] = &cfg
}

// Unregister removes the role configuration for a role. It is the inverse
// of Register and is used by tests to fully restore the pre-test registry
// state — re-registering an empty RoleConfig is not equivalent because
// ConfigFor would still return a non-nil pointer to an empty struct,
// which downstream callers (e.g. dispatch.ActivateWrit) interpret as
// "configured" and will try to use, surfacing a "worktree dir is required"
// failure. Idempotent.
func Unregister(role string) {
	delete(registry, role)
}

// ConfigFor returns the registered RoleConfig for a role.
// Returns nil if no config is registered.
func ConfigFor(role string) *RoleConfig {
	return registry[role]
}

// Launch executes the universal agent session launch sequence.
// Steps:
//  1. Get worktree directory
//  2. Install persona (cfg.Persona → adapter.InjectPersona)
//  3. Clean up stale .claude/CLAUDE.local.md
//  4. Install skills (cfg.SkillInstaller → adapter.InstallSkills)
//  5. Append persona file content to system prompt
//  6. Install hooks (cfg.Hooks → adapter.InstallHooks)
//  7. Execute SessionStart hooks inline
//  8. Ensure config dir + pre-trust (adapter.EnsureConfigDir)
//  9. Ensure agent record in sphere store
//  10. Build prime context (cfg.PrimeBuilder)
//  11. Build session command (adapter.BuildCommand)
//  12. Read credentials
//  13. Build session environment
//  14. Start (or cycle) tmux session
func Launch(cfg RoleConfig, world, agent string, opts LaunchOpts) (sessName string, retErr error) {
	sessName = config.SessionName(world, agent)

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

	// Load world config (needed for model resolution and adapter selection).
	var worldCfg config.WorldConfig
	if opts.WorldConfig != nil {
		worldCfg = *opts.WorldConfig
	} else {
		var err error
		worldCfg, err = config.LoadWorldConfig(world)
		if err != nil {
			return "", fmt.Errorf("startup: failed to load world config: %w", err)
		}
	}

	// Share the loaded config with callbacks so they don't reload independently.
	if cfg.WorldConfigHook != nil {
		cfg.WorldConfigHook(&worldCfg)
	}

	// Resolve runtime adapter.
	a := cfg.Adapter
	if a == nil {
		runtime := worldCfg.ResolveRuntime(cfg.Role)
		var ok bool
		a, ok = adapter.Get(runtime)
		if !ok {
			return "", fmt.Errorf("startup: unknown runtime %q for role %q", runtime, cfg.Role)
		}
	}

	// Guard: fail early if the session is already running (skip when SessionOp
	// is set — that path performs an atomic cycle and the session is expected
	// to exist).
	//
	// NOTE: This Exists check has a TOCTOU window when called via Respawn
	// (SessionOp is nil). Cast pre-cleans stale sessions before calling Launch,
	// but Respawn does not. A concurrent Resolve + Respawn could race: Resolve
	// stops the session, Exists returns false, then another caller starts a
	// session before this Launch reaches step 14. The impact is a temporary
	// duplicate session that resolves at the next prefect patrol.
	if opts.SessionOp == nil {
		mgr := resolveSessionStarter(opts)
		if mgr.Exists(sessName) {
			return "", fmt.Errorf("session already running: %s", sessName)
		}
	}

	// 2. Install persona (CLAUDE.local.md).
	if cfg.Persona != nil {
		content, err := cfg.Persona(world, agent)
		if err != nil {
			return "", fmt.Errorf("startup: failed to generate persona: %w", err)
		}
		if err := a.InjectPersona(worktreeDir, content); err != nil {
			return "", fmt.Errorf("startup: failed to install persona: %w", err)
		}
	}

	// 3. Clean up stale .claude/CLAUDE.local.md from older code path.
	// The canonical location is now {worktreeRoot}/CLAUDE.local.md (written above).
	stalePath := filepath.Join(worktreeDir, ".claude", "CLAUDE.local.md")
	if err := os.Remove(stalePath); err != nil && !os.IsNotExist(err) {
		slog.Warn("startup: failed to remove stale .claude/CLAUDE.local.md", "path", stalePath, "error", err)
	}

	// 4. Install skills (.claude/skills/).
	if cfg.SkillInstaller != nil {
		skills := cfg.SkillInstaller(world, agent)
		if err := a.InstallSkills(worktreeDir, skills); err != nil {
			return "", fmt.Errorf("startup: failed to install skills: %w", err)
		}
	}

	// 5. Append persona file content to system prompt if available.
	if cfg.PersonaFile != nil {
		if pf := cfg.PersonaFile(world, agent); pf != "" {
			if data, readErr := os.ReadFile(pf); readErr == nil && len(data) > 0 {
				if cfg.SystemPromptContent != "" {
					cfg.SystemPromptContent += "\n\n## Persona\n" + string(data)
				} else {
					cfg.SystemPromptContent = "## Persona\n" + string(data)
				}
			}
			// Missing file is a no-op (persona files are optional).
		}
	}

	// Install system prompt content if provided.
	systemPromptFile := ""
	if cfg.SystemPromptContent != "" {
		var err error
		systemPromptFile, err = a.InjectSystemPrompt(worktreeDir, cfg.SystemPromptContent, cfg.ReplacePrompt)
		if err != nil {
			return "", fmt.Errorf("startup: failed to inject system prompt: %w", err)
		}
	}

	// 6. Install hooks (settings.local.json).
	var hookSet adapter.HookSet
	if cfg.Hooks != nil {
		hookSet = cfg.Hooks(world, agent)
		if err := a.InstallHooks(worktreeDir, config.WorldDir(world), cfg.Role, agent, hookSet); err != nil {
			return "", fmt.Errorf("startup: failed to install hooks: %w", err)
		}
	}

	// 7. Execute SessionStart hooks inline for adapters that don't support them natively.
	if !a.SupportsHook("SessionStart") && len(hookSet.SessionStart) > 0 {
		output := executeSessionStartHooks(hookSet.SessionStart, worktreeDir, world, agent)
		if output != "" {
			if _, err := a.InjectSystemPrompt(worktreeDir, "\n## Startup Context\n"+output, false); err != nil {
				slog.Warn("startup: failed to inject SessionStart hook output", "error", err)
			}
		}
	}

	// 8. Ensure runtime config dir and pre-trust working directory.
	worldDir := config.WorldDir(world)
	resolvedAccount := opts.Account
	if resolvedAccount == "" {
		resolvedAccount = account.ResolveAccount("", worldCfg.World.DefaultAccount)
	}
	configResult, err := a.EnsureConfigDir(worldDir, cfg.Role, agent, worktreeDir)
	if err != nil {
		return "", fmt.Errorf("startup: failed to ensure config dir: %w", err)
	}

	// 9. Ensure agent record in sphere store.
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
		if !errors.Is(getErr, store.ErrNotFound) {
			return "", fmt.Errorf("startup: failed to check agent record: %w", getErr)
		}
		if _, err := sphereStore.CreateAgent(agent, world, cfg.Role); err != nil {
			return "", fmt.Errorf("startup: failed to register agent: %w", err)
		}
	}
	// Preserve existing tether item (outpost agents have tethered writs).
	activeWrit := ""
	prevState := store.AgentIdle
	if existing != nil {
		activeWrit = existing.ActiveWrit
		prevState = existing.State
	}
	if err := sphereStore.UpdateAgentState(agentID, "working", activeWrit); err != nil {
		return "", fmt.Errorf("startup: failed to set agent working: %w", err)
	}
	// Roll back agent state to its previous value if Launch fails after this
	// point — prevents the agent from being stuck in "working" with no live
	// session (e.g. if the tmux session already exists or credentials fail).
	defer func() {
		if retErr != nil {
			if rbErr := sphereStore.UpdateAgentState(agentID, prevState, activeWrit); rbErr != nil {
				slog.Warn("startup: failed to rollback agent state after launch error",
					"agent", agentID, "error", rbErr)
			}
		}
	}()

	// 10. Build prime context.
	prompt := ""
	if cfg.PrimeBuilder != nil {
		prompt = cfg.PrimeBuilder(world, agent)
	}

	// 11. Build session command via adapter.
	model := worldCfg.ResolveModel(cfg.Role, a.Name())
	if model == "" {
		model = a.DefaultModel()
	}
	sessionCmd := a.BuildCommand(adapter.CommandContext{
		WorktreeDir:      worktreeDir,
		Prompt:           prompt,
		Continue:         opts.Continue,
		Model:            model,
		SystemPromptFile: systemPromptFile,
		ReplacePrompt:    cfg.ReplacePrompt,
	})

	// 12. Read credentials.
	tok, err := account.ReadToken(resolvedAccount)
	if err != nil {
		return "", fmt.Errorf("startup: no token found for account %q — run: sol account set-token %s (or sol account set-api-key %s): %w", resolvedAccount, resolvedAccount, resolvedAccount, err)
	}

	// 13. Build session environment.
	// Load world .env and use it as the base environment; system vars below
	// take precedence so SOL_HOME, CLAUDE_CONFIG_DIR, etc. cannot be overridden.
	dotEnv, err := envfile.LoadEnv(config.Home(), world)
	if err != nil {
		slog.Warn("startup: failed to load world .env, skipping", "world", world, "error", err)
		dotEnv = map[string]string{}
	}
	env := dotEnv

	// System-managed variables always win over .env entries.
	env["SOL_HOME"] = config.Home()
	env["SOL_WORLD"] = world
	env["SOL_AGENT"] = agent

	// Inject config dir env vars (e.g. CLAUDE_CONFIG_DIR).
	for k, v := range configResult.EnvVar {
		env[k] = v
	}

	// Inject credential env vars.
	cred := adapter.Credential{Type: tok.Type, Token: tok.Token}
	credEnv, err := a.CredentialEnv(cred)
	if err != nil {
		return "", fmt.Errorf("startup: %w", err)
	}
	for k, v := range credEnv {
		env[k] = v
	}

	// Install runtime-specific credential files (e.g. Codex auth.json).
	if err := a.InstallCredential(configResult.Dir, cred); err != nil {
		return "", fmt.Errorf("startup: %w", err)
	}

	// Inject telemetry env vars.
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		slog.Warn("startup: failed to load global config for ledger port", "error", err)
	}
	for k, v := range a.TelemetryEnv(globalCfg.Ledger.Port, agent, world, activeWrit, resolvedAccount) {
		env[k] = v
	}

	// 14. Start (or cycle) the tmux session.
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

	// Clear any stale resume state now that a session has started successfully.
	// Respawn already clears after Resume, but Launch can be called directly
	// (e.g. via handoff.Exec) — clearing here ensures no stale resume state
	// survives regardless of entry path.
	if err := ClearResumeState(world, agent, cfg.Role); err != nil {
		slog.Warn("startup: failed to clear resume state after launch", "error", err)
	}

	return sessName, nil
}

// ResumeState captures workflow state for compact handoff recovery.
type ResumeState struct {
	CurrentStep     string // workflow step ID the agent was on
	ClaimedResource string // MR ID or work-in-progress identifier
	Reason          string // why handoff happened: "compact", "manual", "error", "writ-switch"
	Summary         string // predecessor's handoff summary (what was done, what's next)

	// Writ-switch fields (populated when Reason == "writ-switch").
	PreviousActiveWrit string // writ ID that was active before the switch
	NewActiveWrit      string // writ ID that is now active
}

// Resume does everything Launch does but uses --continue for conversation
// continuity and injects workflow state into the prime context.
//
// If opts.WritExists is non-nil, it is forwarded to BuildResumePrime so the
// resume prime can substitute a degraded "deleted/closed" notice when
// state.NewActiveWrit no longer exists in the world store (CD-6).
func Resume(cfg RoleConfig, world, agent string, state ResumeState, opts LaunchOpts) (string, error) {
	opts.Continue = true

	writExists := opts.WritExists
	origPrime := cfg.PrimeBuilder
	cfg.PrimeBuilder = func(w, a string) string {
		base := ""
		if origPrime != nil {
			base = origPrime(w, a)
		}
		if writExists != nil {
			return BuildResumePrime(base, state, writExists)
		}
		return BuildResumePrime(base, state)
	}

	return Launch(cfg, world, agent, opts)
}

// Respawn looks up the registered config for a role and performs a Launch
// (or Resume if a resume-state file exists).
func Respawn(role, world, agent string, opts LaunchOpts) (string, error) {
	cfg := ConfigFor(role)
	if cfg == nil {
		return "", fmt.Errorf("no startup config registered for role %q", role)
	}

	opts.Respawn = true

	resumeState, resumeErr := ReadResumeState(world, agent, role)
	if resumeErr != nil {
		slog.Warn("ignoring corrupt resume state, falling through to launch",
			"agent", agent, "world", world, "error", resumeErr)
	}
	if resumeState != nil {
		slog.Info("found resume state, using startup.Resume",
			"agent", agent, "world", world, "reason", resumeState.Reason)
		sessName, err := Resume(*cfg, world, agent, *resumeState, opts)
		// Always clear resume state — on success it's consumed, on failure
		// it's stale and would cause every subsequent respawn to retry the
		// same bad Resume.
		if clearErr := ClearResumeState(world, agent, role); clearErr != nil {
			slog.Warn("failed to clear resume state", "agent", agent, "world", world, "error", clearErr)
		}
		return sessName, err
	}

	return Launch(*cfg, world, agent, opts)
}

// WritExistsFunc reports whether a writ id is still present in the world
// store. It is used by BuildResumePrime to detect a phantom writ — one that
// was deleted/closed between when the resume_state.json was written and when
// a successor session reads it. Implementations should return false for
// missing writs and true otherwise; transient errors should be treated as
// "exists" (true) so a flaky check does not mask the prime context.
type WritExistsFunc func(writID string) bool

// BuildResumePrime constructs a resume-aware prime prompt that includes
// workflow state and claimed resource information.
//
// The optional writExists validator (CD-6) lets callers detect a phantom
// active writ — one whose id is recorded in the resume state but no longer
// exists in the world store (e.g. it was closed or deleted between when
// dispatch.ActivateWrit wrote the state and when a successor session read
// it). When writExists is supplied and reports false for state.NewActiveWrit,
// the helper substitutes a degraded "writ deleted/closed" notice instead of
// printing the phantom id straight into the prime; this prevents a follow-on
// "writ not found" failure inside `sol prime`.
func BuildResumePrime(base string, state ResumeState, writExists ...WritExistsFunc) string {
	var validator WritExistsFunc
	if len(writExists) > 0 {
		validator = writExists[0]
	}
	// Resolve the new-active-writ display once so writ-switch and
	// non-switch branches share the same phantom-detection logic. An empty
	// state.NewActiveWrit means there is no writ id to validate.
	newActiveWrit := state.NewActiveWrit
	phantom := false
	if newActiveWrit != "" && validator != nil && !validator(newActiveWrit) {
		phantom = true
	}

	var b strings.Builder
	b.WriteString("[RESUME] Session recovery")
	if state.Reason != "" {
		fmt.Fprintf(&b, " (reason: %s)", state.Reason)
	}
	b.WriteString(".\n")

	if state.Summary != "" {
		fmt.Fprintf(&b, "Your predecessor's handoff message: %s\n", state.Summary)
	}

	if state.CurrentStep != "" {
		fmt.Fprintf(&b, "You were on step %s. Resume from there.\n", state.CurrentStep)
	}

	if state.ClaimedResource != "" {
		fmt.Fprintf(&b, "Claimed resource: %s is claimed and in-progress.\n", state.ClaimedResource)
	}

	if phantom {
		fmt.Fprintf(&b, "Note: previously active writ %s was deleted/closed; check tether for current state.\n", newActiveWrit)
	} else if state.Reason == "writ-switch" {
		if state.PreviousActiveWrit != "" && newActiveWrit != "" {
			fmt.Fprintf(&b, "Your active writ has changed to %s. Previous active was %s.\n", newActiveWrit, state.PreviousActiveWrit)
		} else if newActiveWrit != "" {
			fmt.Fprintf(&b, "Your active writ has changed to %s.\n", newActiveWrit)
		}
	} else if newActiveWrit != "" {
		fmt.Fprintf(&b, "Active writ: %s\n", newActiveWrit)
	}

	if base != "" {
		b.WriteString("\n")
		b.WriteString(base)
	}

	return b.String()
}

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
	return fileutil.AtomicWrite(p, data, 0o644)
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
// If opts.Sphere is provided and opts.OwnsSphere is true, the cleanup function
// closes the injected store. If opts.Sphere is nil, a new store is opened and
// always closed via the returned cleanup function.
func resolveSphereStore(opts LaunchOpts) (SphereStore, func(), error) {
	if opts.Sphere != nil {
		if opts.OwnsSphere {
			return opts.Sphere, func() { opts.Sphere.Close() }, nil
		}
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

// executeSessionStartHooks runs each SessionStart hook command inline and
// concatenates their stdout output. Commands run with worktreeDir as cwd and
// SOL_HOME, SOL_WORLD, SOL_AGENT set in the environment. Failures are logged
// as warnings and skipped — the remaining hooks continue executing.
func executeSessionStartHooks(hooks []adapter.HookCommand, worktreeDir, world, agent string) string {
	var buf bytes.Buffer
	for _, hc := range hooks {
		cmd := exec.Command("sh", "-c", hc.Command)
		cmd.Dir = worktreeDir
		cmd.Env = append(os.Environ(),
			"SOL_HOME="+config.Home(),
			"SOL_WORLD="+world,
			"SOL_AGENT="+agent,
		)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Warn("startup: SessionStart hook failed",
				"command", hc.Command, "error", err)
			continue
		}
		if stdout.Len() > 0 {
			buf.Write(stdout.Bytes())
		}
	}
	return strings.TrimSpace(buf.String())
}
