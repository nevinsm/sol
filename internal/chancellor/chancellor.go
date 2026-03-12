package chancellor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/brief"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
)

// --- Directory helpers ---

// ChancellorDir returns the root directory for the chancellor.
// $SOL_HOME/chancellor/
func ChancellorDir() string {
	return filepath.Join(config.Home(), "chancellor")
}

// BriefDir returns the brief directory for the chancellor.
// $SOL_HOME/chancellor/.brief/
func BriefDir() string {
	return filepath.Join(config.Home(), "chancellor", ".brief")
}

// BriefPath returns the chancellor's memory file path.
// $SOL_HOME/chancellor/.brief/memory.md
func BriefPath() string {
	return filepath.Join(config.Home(), "chancellor", ".brief", "memory.md")
}

// SessionName is the fixed tmux session name for the chancellor.
const SessionName = "sol-chancellor"

// --- Interfaces ---

// SessionManager abstracts session operations for Start.
type SessionManager interface {
	Exists(name string) bool
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
}

// StopManager abstracts session operations for Stop.
type StopManager interface {
	brief.GracefulStopManager
}

// --- Start ---

// Start launches the chancellor session.
func Start(mgr SessionManager) error {
	dir := ChancellorDir()
	briefDir := BriefDir()

	// 1. Create chancellor and brief directories.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	// 2. Install persona (CLAUDE.local.md).
	ctx := protocol.ChancellorClaudeMDContext{
		SolBinary: "sol",
	}
	if err := protocol.InstallChancellorClaudeMD(dir, ctx); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	// 3. Install hooks in chancellor directory.
	cfg := RoleConfig()
	hookCfg := cfg.Hooks("", "chancellor")
	if err := protocol.WriteHookSettings(dir, hookCfg); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	// 4. Install system prompt.
	if cfg.SystemPromptContent != "" {
		claudeDir := filepath.Join(dir, ".claude")
		if err := os.MkdirAll(claudeDir, 0o755); err != nil {
			return fmt.Errorf("failed to start chancellor: create .claude dir: %w", err)
		}
		promptPath := filepath.Join(claudeDir, "system-prompt.md")
		if err := os.WriteFile(promptPath, []byte(cfg.SystemPromptContent), 0o644); err != nil {
			return fmt.Errorf("failed to start chancellor: write system prompt: %w", err)
		}
	}

	// 5. Check if session already exists.
	if mgr.Exists(SessionName) {
		return fmt.Errorf("chancellor session already running")
	}

	// 6. Resolve account and CLAUDE_CONFIG_DIR.
	resolvedAccount := account.ResolveAccount("", "")
	claudeConfigDir, err := config.EnsureClaudeConfigDir(config.Home(), "chancellor", "chancellor", resolvedAccount)
	if err != nil {
		return fmt.Errorf("failed to ensure claude config dir: %w", err)
	}

	// 7. Pre-trust working directory.
	if err := protocol.TrustDirectoryIn(dir, claudeConfigDir); err != nil {
		fmt.Fprintf(os.Stderr, "chancellor: failed to pre-trust directory: %v\n", err)
	}

	// 8. Read token for credential injection.
	tok, err := account.ReadToken(resolvedAccount)
	if err != nil {
		return fmt.Errorf("no token found for account %q — run: sol account set-token %s (or sol account set-api-key %s): %w",
			resolvedAccount, resolvedAccount, resolvedAccount, err)
	}

	// 9. Build session command and environment.
	prompt := cfg.PrimeBuilder("", "chancellor")
	settingsPath := config.SettingsPath(dir)

	var sessionCmd string
	if cfg.SystemPromptContent != "" {
		promptFile := ".claude/system-prompt.md"
		sessionCmd = buildCommandWithPromptFile(settingsPath, prompt, promptFile, cfg.ReplacePrompt)
	} else {
		sessionCmd = config.BuildSessionCommand(settingsPath, prompt)
	}

	env := map[string]string{
		"SOL_HOME":          config.Home(),
		"SOL_AGENT":         "chancellor",
		"CLAUDE_CONFIG_DIR": claudeConfigDir,
	}
	switch tok.Type {
	case "oauth_token":
		env["CLAUDE_CODE_OAUTH_TOKEN"] = tok.Token
	case "api_key":
		env["ANTHROPIC_API_KEY"] = tok.Token
	}

	// 10. Start tmux session.
	if err := mgr.Start(SessionName, dir, sessionCmd, env, "chancellor", ""); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	return nil
}

// buildCommandWithPromptFile constructs a claude startup command with a system prompt file.
func buildCommandWithPromptFile(settingsPath, prompt, promptFile string, replace bool) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}

	args := "claude --dangerously-skip-permissions"
	args += " --settings " + config.ShellQuote(settingsPath)

	if replace {
		args += " --system-prompt-file " + config.ShellQuote(promptFile)
	} else {
		args += " --append-system-prompt-file " + config.ShellQuote(promptFile)
	}

	if prompt != "" {
		args += " " + config.ShellQuote(prompt)
	}

	return args
}

// --- Stop ---

// Stop terminates the chancellor session gracefully.
func Stop(mgr StopManager) error {
	if !mgr.Exists(SessionName) {
		return fmt.Errorf("no chancellor session running")
	}

	if err := brief.GracefulStop(SessionName, BriefDir(), mgr); err != nil {
		return fmt.Errorf("failed to stop chancellor: %w", err)
	}

	return nil
}
