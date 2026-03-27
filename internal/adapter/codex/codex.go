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

// InjectPersona writes persona content to AGENTS.override.md at the worktree root.
// Codex checks AGENTS.override.md before AGENTS.md at each directory level (first
// match wins), so AGENTS.override.md shadows AGENTS.md. To preserve the project's
// checked-in AGENTS.md, we read it and prepend its content to the override file.
//
// Design decision: We use AGENTS.override.md (with concatenation) rather than
// config.toml's `instructions` field because Codex's `instructions` field maps to
// base_instructions (model instruction overrides), NOT supplementary project
// instructions. It does not supplement user_instructions from AGENTS.md discovery.
func (a *Adapter) InjectPersona(worktreeDir string, content []byte) error {
	path := filepath.Join(worktreeDir, "AGENTS.override.md")

	// Read the project's checked-in AGENTS.md so its content isn't lost when
	// the override file shadows it.
	combined := prependProjectAgentsMD(worktreeDir, content)

	if err := os.WriteFile(path, combined, 0o644); err != nil {
		return fmt.Errorf("codex adapter: failed to write AGENTS.override.md: %w", err)
	}
	return nil
}

// prependProjectAgentsMD reads the project's AGENTS.md from worktreeDir and
// prepends it to content with a separator. If AGENTS.md doesn't exist or is
// empty, returns content unchanged.
func prependProjectAgentsMD(worktreeDir string, content []byte) []byte {
	projectPath := filepath.Join(worktreeDir, "AGENTS.md")
	projectContent, err := os.ReadFile(projectPath)
	if err != nil || len(strings.TrimSpace(string(projectContent))) == 0 {
		return content
	}

	log.Printf("codex adapter: incorporating project AGENTS.md content into AGENTS.override.md")

	var buf strings.Builder
	buf.Write(projectContent)
	if !strings.HasSuffix(string(projectContent), "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteString("\n---\n\n")
	buf.Write(content)
	return []byte(buf.String())
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

// InjectSystemPrompt writes system prompt content to AGENTS.override.md at the
// worktree root. Both replace and append modes write to the same file.
//
// When replace is true, the system prompt replaces all persona content. Because
// AGENTS.override.md shadows AGENTS.md, we prepend the project's checked-in
// AGENTS.md content to preserve project instructions.
//
// When replace is false (append), appends to AGENTS.override.md at the worktree
// root. Previous implementation wrote to .codex/AGENTS.override.md which Codex's
// upward-walk discovery wouldn't find unless cwd was inside .codex/.
//
// Returns the relative path to the written file.
func (a *Adapter) InjectSystemPrompt(worktreeDir, content string, replace bool) (string, error) {
	path := filepath.Join(worktreeDir, "AGENTS.override.md")

	if replace {
		// Prepend project AGENTS.md so it isn't lost via shadowing.
		combined := prependProjectAgentsMD(worktreeDir, []byte(content))
		if err := os.WriteFile(path, combined, 0o644); err != nil {
			return "", fmt.Errorf("codex adapter: failed to write AGENTS.override.md: %w", err)
		}
		return "AGENTS.override.md", nil
	}

	// Append mode: read existing override file and append content.
	existing, _ := os.ReadFile(path)
	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			buf.WriteByte('\n')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(content)
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		return "", fmt.Errorf("codex adapter: failed to write AGENTS.override.md: %w", err)
	}
	return "AGENTS.override.md", nil
}

// InstallHooks performs best-effort translation of the runtime-agnostic HookSet
// into Codex-native config and AGENTS.override.md instruction text.
//
// Mapping:
//   - Guards       → "IMPORTANT: NEVER run: {pattern}" lines in AGENTS.override.md
//   - PreCompact   → "Before running /compact, execute this command: {command}" lines
//   - TurnBoundary → First hook written as `notify` in project-level .codex/config.toml;
//     remaining hooks written as "Periodically run this command: {command}" lines
//   - SessionStart → Logged warning (shell hooks not translatable to instructions)
//
// Appends to existing AGENTS.override.md content (the persona/override file,
// not the project's AGENTS.md).
// Returns nil always — degradation is acceptable.
func (a *Adapter) InstallHooks(worktreeDir string, hooks adapter.HookSet) error {
	// SessionStart hooks run as shell commands at launch — not translatable to
	// agent instructions. Log a warning and skip.
	if len(hooks.SessionStart) > 0 {
		log.Printf("codex adapter: SessionStart hooks are not supported for Codex runtime (%d hooks skipped)", len(hooks.SessionStart))
	}

	// Write the first TurnBoundary hook as a native notify command in the
	// project-level .codex/config.toml. Codex executes this after each turn
	// with a JSON payload — a real hook mechanism.
	if len(hooks.TurnBoundary) > 0 {
		notifyCmd := hooks.TurnBoundary[0].Command
		// Build notify as a TOML array: split command string into argv.
		notifyLine := fmt.Sprintf("notify = %s\n", toTOMLStringArray(strings.Fields(notifyCmd)))
		if err := appendToProjectConfig(worktreeDir, notifyLine); err != nil {
			log.Printf("codex adapter: failed to write notify to project config: %v", err)
		}
		if len(hooks.TurnBoundary) > 1 {
			log.Printf("codex adapter: WARNING: %d TurnBoundary hooks provided but only the first is used as notify; remaining %d hooks will be instruction text",
				len(hooks.TurnBoundary), len(hooks.TurnBoundary)-1)
		}
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

	// Remaining TurnBoundary hooks (skip first — it's the notify command).
	for i, hc := range hooks.TurnBoundary {
		if i == 0 {
			continue // already written as notify
		}
		instructions = append(instructions, fmt.Sprintf("Periodically run this command: %s", hc.Command))
	}

	if len(instructions) == 0 {
		return nil
	}

	// Append to AGENTS.override.md (the persona/sol-managed override file).
	overridePath := filepath.Join(worktreeDir, "AGENTS.override.md")
	existing, _ := os.ReadFile(overridePath) // ignore error — file may not exist yet

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

	if err := os.WriteFile(overridePath, []byte(buf.String()), 0o644); err != nil {
		// Best-effort: log but don't fail.
		log.Printf("codex adapter: failed to append hooks to AGENTS.override.md: %v", err)
	}
	return nil
}

// appendToProjectConfig reads the project-level .codex/config.toml, appends
// the given content, and writes it back. Creates the file and directory if
// they don't exist. This is safe for read-modify-write across multiple
// startup steps (e.g. InstallSkills writes skills, InstallHooks writes notify).
func appendToProjectConfig(worktreeDir, content string) error {
	configDir := filepath.Join(worktreeDir, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("codex adapter: failed to create .codex dir: %w", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	existing, _ := os.ReadFile(configPath) // ignore error — file may not exist yet

	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString(content)

	if err := os.WriteFile(configPath, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("codex adapter: failed to write .codex/config.toml: %w", err)
	}
	return nil
}

// toTOMLStringArray converts a slice of strings to a TOML inline array string.
// e.g. ["sol", "heartbeat"] → `["sol", "heartbeat"]`
func toTOMLStringArray(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
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

// EnsureConfigDir creates a per-agent CODEX_HOME directory at
// {worldDir}/{role}s/{agent}/.codex-home/ and writes config.toml with
// hardened settings for automated sessions. Writes an [otel] section
// pointing to sol's ledger when a ledger port is configured.
// Returns CODEX_HOME and CODEX_SQLITE_HOME in EnvVar so the session
// environment picks them up.
func (a *Adapter) EnsureConfigDir(worldDir, role, agent, worktreeDir string) (adapter.ConfigResult, error) {
	// Per-agent isolation: each agent gets its own CODEX_HOME so concurrent
	// agents don't clobber each other's config.
	dir := filepath.Join(worldDir, role+"s", agent, ".codex-home")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return adapter.ConfigResult{}, fmt.Errorf("codex adapter: failed to create .codex-home dir: %w", err)
	}

	// Build config.toml content.
	var buf strings.Builder

	// approval_policy = "never": never ask the user for approval; failures go
	// back to the model (AskForApproval::Never in Codex source).
	// sandbox_mode = "danger-full-access": no sandbox restrictions
	// (SandboxMode::DangerFullAccess). Together these are equivalent to
	// --dangerously-bypass-approvals-and-sandbox.
	buf.WriteString("approval_policy = \"never\"\n")
	buf.WriteString("sandbox_mode = \"danger-full-access\"\n")

	// Disable URI-based file opener (no IDE in headless sessions).
	buf.WriteString("file_opener = \"none\"\n")

	// Use file-based auth credential store (keyring unavailable in headless tmux).
	buf.WriteString("cli_auth_credentials_store = \"file\"\n")

	// Double the default project doc max bytes (32 KiB → 64 KiB) to prevent
	// silent truncation of large AGENTS.md / project docs.
	buf.WriteString("project_doc_max_bytes = 65536\n")

	// Disable built-in memories — sol uses the brief system instead.
	buf.WriteString("\n[memories]\n")
	buf.WriteString("generate_memories = false\n")
	buf.WriteString("use_memories = false\n")

	// Disable TUI features that interfere with automated sessions.
	buf.WriteString("\n[tui]\n")
	buf.WriteString("animations = false\n")
	buf.WriteString("show_tooltips = false\n")
	// Keep output visible for health monitoring pane capture.
	buf.WriteString("alternate_screen = \"never\"\n")
	buf.WriteString("notifications = false\n")

	// Add [otel] section if ledger port is configured.
	globalCfg, cfgErr := config.LoadGlobalConfig()
	if cfgErr == nil && globalCfg.Ledger.Port > 0 {
		buf.WriteString("\n[otel.exporter.otlp-http]\n")
		fmt.Fprintf(&buf, "endpoint = \"http://localhost:%d/v1/logs\"\n", globalCfg.Ledger.Port)
		buf.WriteString("protocol = \"json\"\n")
	}

	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(buf.String()), 0o644); err != nil {
		return adapter.ConfigResult{}, fmt.Errorf("codex adapter: failed to write config.toml: %w", err)
	}

	return adapter.ConfigResult{
		Dir: dir,
		EnvVar: map[string]string{
			"CODEX_HOME":        dir,
			"CODEX_SQLITE_HOME": dir,
		},
	}, nil
}

// BuildCommand constructs the codex startup command string.
//
// Format:
//
//	codex [--dangerously-bypass-approvals-and-sandbox] [--model <model>] ["<prompt>"]
//
// If ctx.Continue is true, returns: codex resume --last ["<prompt>"]
// If SOL_SESSION_COMMAND is set (for testing), it is returned verbatim.
func (a *Adapter) BuildCommand(ctx adapter.CommandContext) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}

	if ctx.Continue {
		cmd := "codex resume --last"
		if ctx.Prompt != "" {
			cmd += " " + config.ShellQuote(ctx.Prompt)
		}
		return cmd
	}

	args := "codex --dangerously-bypass-approvals-and-sandbox"

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

// TelemetryEnv returns OTEL_RESOURCE_ATTRIBUTES for Codex sessions. Codex's
// own OTEL instrumentation builds resource attributes programmatically and
// does not read OTEL_RESOURCE_ATTRIBUTES, but the standard env var is set
// for consistency with the Claude adapter and for any downstream OTEL
// tooling that respects the convention.
func (a *Adapter) TelemetryEnv(port int, agent, world, activeWrit, account string) map[string]string {
	if port <= 0 {
		return map[string]string{}
	}
	attrs := fmt.Sprintf("agent.name=%s,world=%s", agent, world)
	if activeWrit != "" {
		attrs += ",writ_id=" + activeWrit
	}
	if account != "" {
		attrs += ",account=" + account
	}
	attrs += ",service.name=codex"
	return map[string]string{
		"OTEL_RESOURCE_ATTRIBUTES": attrs,
	}
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
