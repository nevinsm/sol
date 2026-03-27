// Package codex implements the RuntimeAdapter interface for the OpenAI Codex runtime.
package codex

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/config"
)

func init() {
	adapter.Register("codex", New())
}

// Adapter implements adapter.RuntimeAdapter for the Codex runtime.
type Adapter struct{}

// Compile-time interface satisfaction check.
var _ adapter.RuntimeAdapter = (*Adapter)(nil)

// New returns a new codex Adapter.
func New() *Adapter {
	return &Adapter{}
}

// Name returns "codex".
func (a *Adapter) Name() string {
	return "codex"
}

// CalloutCommand returns "codex exec --json", the default one-shot invocation
// command for the Codex runtime.
func (a *Adapter) CalloutCommand() string {
	return "codex exec --json"
}

// InjectPersona writes persona content to AGENTS.md at the worktree root.
// This is a minimal stub — full merge logic is implemented in a follow-up writ.
func (a *Adapter) InjectPersona(worktreeDir string, content []byte) error {
	path := filepath.Join(worktreeDir, "AGENTS.md")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("codex adapter: failed to write AGENTS.md: %w", err)
	}
	return nil
}

// InstallSkills is a no-op stub.
// TODO(follow-up): Implement Codex skill installation to .agents/skills/.
func (a *Adapter) InstallSkills(_ string, _ []adapter.Skill) error {
	return nil
}

// InjectSystemPrompt writes system prompt content to the worktree.
// When replace is true, writes to {worktreeDir}/AGENTS.md.
// When replace is false (append), writes to {worktreeDir}/.codex/AGENTS.override.md.
// Returns the relative path to the written file.
// This is a minimal stub — full merge logic is implemented in a follow-up writ.
func (a *Adapter) InjectSystemPrompt(worktreeDir, content string, replace bool) (string, error) {
	if replace {
		path := filepath.Join(worktreeDir, "AGENTS.md")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("codex adapter: failed to write AGENTS.md: %w", err)
		}
		return "AGENTS.md", nil
	}

	codexDir := filepath.Join(worktreeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return "", fmt.Errorf("codex adapter: failed to create .codex directory: %w", err)
	}
	path := filepath.Join(codexDir, "AGENTS.override.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("codex adapter: failed to write AGENTS.override.md: %w", err)
	}
	return ".codex/AGENTS.override.md", nil
}

// InstallHooks is a no-op stub.
// TODO(follow-up): Implement Codex hook installation (translate HookSet to Codex format).
func (a *Adapter) InstallHooks(_ string, _ adapter.HookSet) error {
	return nil
}

// EnsureConfigDir creates the ~/.codex/ directory and writes config.toml with
// approval_policy = "never" and sandbox_mode = "workspace-write". Writes an
// [otel] section pointing to sol's ledger when a ledger port is configured.
// Returns env vars to inject into the session.
func (a *Adapter) EnsureConfigDir(worldDir, role, agent, worktreeDir string) (adapter.ConfigResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return adapter.ConfigResult{}, fmt.Errorf("codex adapter: failed to get home dir: %w", err)
	}

	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return adapter.ConfigResult{}, fmt.Errorf("codex adapter: failed to create .codex dir: %w", err)
	}

	// Build config.toml content.
	tomlContent := "approval_policy = \"never\"\nsandbox_mode = \"workspace-write\"\n"

	// Add [otel] section if ledger port is configured.
	globalCfg, cfgErr := config.LoadGlobalConfig()
	if cfgErr == nil && globalCfg.Ledger.Port > 0 {
		tomlContent += fmt.Sprintf("\n[otel]\nendpoint = \"http://localhost:%d/v1/logs\"\n", globalCfg.Ledger.Port)
	}

	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(tomlContent), 0o644); err != nil {
		return adapter.ConfigResult{}, fmt.Errorf("codex adapter: failed to write config.toml: %w", err)
	}

	return adapter.ConfigResult{
		Dir:    dir,
		EnvVar: map[string]string{},
	}, nil
}

// BuildCommand constructs the codex startup command string.
//
// Format:
//
//	codex [--full-auto] [--model <model>] ["<prompt>"]
//
// If ctx.Continue is true, returns: codex resume
// If SOL_SESSION_COMMAND is set (for testing), it is returned verbatim.
func (a *Adapter) BuildCommand(ctx adapter.CommandContext) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}

	if ctx.Continue {
		return "codex resume"
	}

	args := "codex --full-auto"

	if ctx.Model != "" {
		args += " --model " + ctx.Model
	}

	if ctx.Prompt != "" {
		args += " " + config.ShellQuote(ctx.Prompt)
	}

	return args
}

// CredentialEnv returns the environment variable map for the given credential.
//   - "api_key" → OPENAI_API_KEY
//
// Returns an error for unrecognized credential types.
func (a *Adapter) CredentialEnv(cred adapter.Credential) (map[string]string, error) {
	switch cred.Type {
	case "api_key":
		return map[string]string{"OPENAI_API_KEY": cred.Token}, nil
	default:
		return nil, fmt.Errorf("unrecognized credential type %q — no credentials set; session will fail authentication", cred.Type)
	}
}

// TelemetryEnv returns an empty map. Codex OTEL configuration is file-based,
// written in EnsureConfigDir via config.toml's [otel] section.
func (a *Adapter) TelemetryEnv(port int, agent, world, activeWrit, account string) map[string]string {
	return map[string]string{}
}

// ExtractTelemetry returns nil.
// TODO(follow-up): Investigate Codex OTEL event format and implement extraction.
func (a *Adapter) ExtractTelemetry(eventName string, attrs map[string]string) *adapter.TelemetryRecord {
	return nil
}
