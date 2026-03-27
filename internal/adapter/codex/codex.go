// Package codex implements the RuntimeAdapter interface for the OpenAI Codex runtime.
package codex

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
// Codex discovers AGENTS.md via directory walk from cwd upward. This is the
// persona file (equivalent of CLAUDE.local.md for the Claude adapter).
func (a *Adapter) InjectPersona(worktreeDir string, content []byte) error {
	path := filepath.Join(worktreeDir, "AGENTS.md")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("codex adapter: failed to write AGENTS.md: %w", err)
	}
	return nil
}

// solManagedMarker is the filename placed inside sol-generated skill directories
// to distinguish them from custom project skills. Only directories containing
// this marker are candidates for stale-skill removal.
const solManagedMarker = ".sol-managed"

// InstallSkills writes skill files to {worktreeDir}/.agents/skills/{name}/SKILL.md.
// Stale sol-managed skill directories (present on disk but not in the skills list)
// are removed. Non-sol directories (those without a .sol-managed marker) are preserved.
//
// Skills are written before stale directories are removed so that a write failure
// (e.g. disk full) leaves the previous skills intact.
func (a *Adapter) InstallSkills(worktreeDir string, skills []adapter.Skill) error {
	skillsDir := filepath.Join(worktreeDir, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("codex adapter: failed to create skills directory: %w", err)
	}

	// Build set of incoming skill names.
	current := make(map[string]bool, len(skills))
	for _, s := range skills {
		current[s.Name] = true
	}

	// Write each skill first — if this fails, old skills remain on disk.
	for _, s := range skills {
		skillDir := filepath.Join(skillsDir, s.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("codex adapter: failed to create skill dir %q: %w", s.Name, err)
		}
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(s.Content), 0o644); err != nil {
			return fmt.Errorf("codex adapter: failed to write skill %q: %w", s.Name, err)
		}
		// Mark directory as sol-managed.
		markerPath := filepath.Join(skillDir, solManagedMarker)
		if err := os.WriteFile(markerPath, nil, 0o644); err != nil {
			return fmt.Errorf("codex adapter: failed to write sol-managed marker for skill %q: %w", s.Name, err)
		}
	}

	// Remove stale sol-managed skill directories.
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return fmt.Errorf("codex adapter: failed to read skills directory: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || current[e.Name()] {
			continue
		}
		// Only remove directories that sol created (have the marker).
		markerPath := filepath.Join(skillsDir, e.Name(), solManagedMarker)
		if _, err := os.Stat(markerPath); err != nil {
			continue // not sol-managed — preserve it
		}
		stale := filepath.Join(skillsDir, e.Name())
		if err := os.RemoveAll(stale); err != nil {
			return fmt.Errorf("codex adapter: failed to remove stale skill %q: %w", e.Name(), err)
		}
	}

	return nil
}

// InjectSystemPrompt writes system prompt content to the worktree.
// When replace is true, writes to {worktreeDir}/AGENTS.md (overwrites persona —
// this is correct for outpost agents that get their entire context as system prompt).
// When replace is false (append), writes to {worktreeDir}/.codex/AGENTS.override.md
// so it appends via Codex's discovery mechanism.
// Returns the relative path to the written file.
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

// InstallHooks performs best-effort translation of the runtime-agnostic HookSet
// into AGENTS.md instruction text, since Codex has no lifecycle hook mechanism.
//
// Mapping:
//   - Guards      → "IMPORTANT: NEVER run: {pattern}" lines in AGENTS.md
//   - PreCompact  → "Before running /compact, execute this command: {command}" lines
//   - TurnBoundary → "Periodically run this command: {command}" lines
//   - SessionStart → Logged warning (shell hooks not translatable to instructions)
//
// Appends to existing AGENTS.md content (does not overwrite persona).
// Returns nil always — degradation is acceptable.
func (a *Adapter) InstallHooks(worktreeDir string, hooks adapter.HookSet) error {
	// SessionStart hooks run as shell commands at launch — not translatable to
	// agent instructions. Log a warning and skip.
	if len(hooks.SessionStart) > 0 {
		log.Printf("codex adapter: SessionStart hooks are not supported for Codex runtime (%d hooks skipped)", len(hooks.SessionStart))
	}

	var instructions []string

	// Translate Guards into instruction text.
	for _, g := range hooks.Guards {
		// Strip tool-call wrapper syntax to get the human-readable command.
		// e.g. "Bash(git push --force*)" → "git push --force"
		readable := extractGuardReadable(g.Pattern)
		instructions = append(instructions, fmt.Sprintf("IMPORTANT: NEVER run: %s", readable))
	}

	// Translate PreCompact hooks.
	for _, hc := range hooks.PreCompact {
		instructions = append(instructions, fmt.Sprintf("Before running /compact, execute this command: %s", hc.Command))
	}

	// Translate TurnBoundary hooks.
	for _, hc := range hooks.TurnBoundary {
		instructions = append(instructions, fmt.Sprintf("Periodically run this command: %s", hc.Command))
	}

	if len(instructions) == 0 {
		return nil
	}

	// Append to existing AGENTS.md content.
	agentsPath := filepath.Join(worktreeDir, "AGENTS.md")
	existing, _ := os.ReadFile(agentsPath) // ignore error — file may not exist yet

	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		// Ensure separation from existing content.
		if !strings.HasSuffix(string(existing), "\n") {
			buf.WriteByte('\n')
		}
		buf.WriteByte('\n')
	}

	for _, instr := range instructions {
		buf.WriteString(instr)
		buf.WriteByte('\n')
	}

	if err := os.WriteFile(agentsPath, []byte(buf.String()), 0o644); err != nil {
		// Best-effort: log but don't fail.
		log.Printf("codex adapter: failed to append hooks to AGENTS.md: %v", err)
	}
	return nil
}

// extractGuardReadable converts a guard pattern like "Bash(git push --force*)"
// into a human-readable command "git push --force". Strips tool-call wrappers
// and trailing glob wildcards.
func extractGuardReadable(pattern string) string {
	// Check for "ToolName(args)" format.
	if _, inner, ok := strings.Cut(pattern, "("); ok {
		// Strip trailing ")" and optional "*".
		inner = strings.TrimRight(inner, ")*")
		inner = strings.TrimSpace(inner)
		if inner != "" {
			return inner
		}
	}
	// No wrapper — return the pattern as-is, minus trailing wildcards.
	return strings.TrimRight(pattern, "*")
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

// ExtractTelemetry extracts token usage data from a Codex OTEL log event.
// Accepts events named "codex.api_request" or "gen_ai.content.completion".
// Returns nil if the event is not relevant or has no model information.
//
// TODO: Verify exact Codex OTEL event names and attribute keys at runtime.
// Current attribute names are based on OpenTelemetry gen_ai.* semantic conventions.
func (a *Adapter) ExtractTelemetry(eventName string, attrs map[string]string) *adapter.TelemetryRecord {
	// TODO: Verify exact Codex event names — these are reasonable defaults
	// based on OpenTelemetry semantic conventions for generative AI.
	if eventName != "codex.api_request" && eventName != "gen_ai.content.completion" {
		return nil
	}

	// Extract model — try gen_ai.* convention first, then short name fallback.
	model := attrs["gen_ai.response.model"]
	if model == "" {
		model = attrs["gen_ai.request.model"]
	}
	if model == "" {
		model = attrs["model"]
	}
	if model == "" {
		return nil
	}

	// TODO: Verify exact Codex OTEL attribute names for token counts.
	input := parseIntAttr(attrs, "gen_ai.usage.input_tokens")
	if input == 0 {
		input = parseIntAttr(attrs, "input_tokens")
	}
	output := parseIntAttr(attrs, "gen_ai.usage.output_tokens")
	if output == 0 {
		output = parseIntAttr(attrs, "output_tokens")
	}

	// TODO: Verify if Codex reports cache tokens via OTEL.
	cacheRead := parseIntAttr(attrs, "gen_ai.usage.cache_read_input_tokens")
	cacheCreation := parseIntAttr(attrs, "gen_ai.usage.cache_creation_input_tokens")

	costUSD := parseFloatAttr(attrs, "cost_usd")
	durationMS := parseIntPtrAttr(attrs, "duration_ms")

	return &adapter.TelemetryRecord{
		Model:               model,
		InputTokens:         input,
		OutputTokens:        output,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreation,
		CostUSD:             costUSD,
		DurationMS:          durationMS,
	}
}

// parseIntAttr parses an integer attribute value, returning 0 on failure.
func parseIntAttr(attrs map[string]string, key string) int64 {
	v, ok := attrs[key]
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseFloatAttr parses a float attribute value, returning nil if absent or invalid.
func parseFloatAttr(attrs map[string]string, key string) *float64 {
	v, ok := attrs[key]
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

// parseIntPtrAttr parses an integer attribute value, returning nil if absent or invalid.
func parseIntPtrAttr(attrs map[string]string, key string) *int64 {
	v, ok := attrs[key]
	if !ok {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
