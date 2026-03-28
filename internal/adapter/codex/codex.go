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

// SupportsHook reports whether the Codex adapter handles the given hook type
// natively. Codex supports TurnBoundary (via notify) and Guard (via exec
// policy rules) natively; SessionStart and PreCompact are instruction-text only.
func (a *Adapter) SupportsHook(hookType string) bool {
	switch hookType {
	case "TurnBoundary":
		return true // first hook becomes native notify
	case "Guard":
		return true // exec policy rules
	default:
		return false // SessionStart, PreCompact → instruction text only
	}
}

// CalloutCommand returns "codex --json", the default one-shot invocation
// command for the Codex runtime. The prompt is passed as a positional argument
// and --json emits JSONL events to stdout for structured parsing.
func (a *Adapter) CalloutCommand() string {
	return "codex --json"
}

// Section markers for AGENTS.override.md. Each adapter method writes to its own
// section so that InjectPersona, InjectSystemPrompt, and InstallHooks don't
// clobber each other's content.
const (
	sectionProject     = "SOL:PROJECT"
	sectionPersona     = "SOL:PERSONA"
	sectionSystemPrompt = "SOL:SYSTEM-PROMPT"
	sectionHooks       = "SOL:HOOKS"
)

// sectionOrder defines the canonical ordering of sections in AGENTS.override.md.
var sectionOrder = []string{sectionProject, sectionPersona, sectionSystemPrompt, sectionHooks}

// parseSections reads an AGENTS.override.md file and splits it into named
// sections keyed by marker name (e.g. "SOL:PROJECT"). Content before the first
// marker is discarded (shouldn't exist in well-formed files). Returns an empty
// map if the file doesn't exist or has no markers.
func parseSections(data string) map[string]string {
	sections := make(map[string]string)
	var currentSection string
	var buf strings.Builder

	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<!-- ") && strings.HasSuffix(trimmed, " -->") {
			// Flush previous section.
			if currentSection != "" {
				sections[currentSection] = buf.String()
				buf.Reset()
			}
			marker := strings.TrimPrefix(trimmed, "<!-- ")
			marker = strings.TrimSuffix(marker, " -->")
			currentSection = marker
			continue
		}
		if currentSection != "" {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	// Flush final section.
	if currentSection != "" {
		sections[currentSection] = buf.String()
	}
	return sections
}

// renderSections assembles the sections map into the final AGENTS.override.md
// content. Sections are emitted in sectionOrder; empty sections are skipped.
func renderSections(sections map[string]string) string {
	var buf strings.Builder
	first := true
	for _, name := range sectionOrder {
		content, ok := sections[name]
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		if !first {
			buf.WriteByte('\n')
		}
		fmt.Fprintf(&buf, "<!-- %s -->\n", name)
		// Ensure content ends with a single newline.
		content = strings.TrimRight(content, "\n") + "\n"
		buf.WriteString(content)
		first = false
	}
	return buf.String()
}

// updateSection reads AGENTS.override.md, updates the named section, and writes
// the file back. If the file doesn't exist, it is created.
func updateSection(worktreeDir, sectionName, content string) error {
	path := filepath.Join(worktreeDir, "AGENTS.override.md")
	existing, _ := os.ReadFile(path) // ignore error — file may not exist yet

	sections := parseSections(string(existing))
	sections[sectionName] = content

	rendered := renderSections(sections)
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("codex adapter: failed to write AGENTS.override.md: %w", err)
	}
	return nil
}

// InjectPersona writes persona content to the PERSONA section of
// AGENTS.override.md at the worktree root. If a project AGENTS.md exists,
// its content is written to the PROJECT section so it isn't lost when the
// override file shadows it.
//
// Design decision: We use AGENTS.override.md (with concatenation) rather than
// config.toml's `instructions` field because Codex's `instructions` field maps to
// base_instructions (model instruction overrides), NOT supplementary project
// instructions. It does not supplement user_instructions from AGENTS.md discovery.
func (a *Adapter) InjectPersona(worktreeDir string, content []byte) error {
	path := filepath.Join(worktreeDir, "AGENTS.override.md")
	existing, _ := os.ReadFile(path)
	sections := parseSections(string(existing))

	// Write project AGENTS.md to the PROJECT section.
	projectContent := readProjectAgentsMD(worktreeDir)
	if projectContent != "" {
		sections[sectionProject] = projectContent
		log.Printf("codex adapter: incorporating project AGENTS.md content into AGENTS.override.md")
	}

	sections[sectionPersona] = string(content)

	rendered := renderSections(sections)
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("codex adapter: failed to write AGENTS.override.md: %w", err)
	}
	return nil
}

// readProjectAgentsMD reads the project's AGENTS.md from worktreeDir and
// returns its content as a string. Returns "" if the file doesn't exist or
// is empty.
func readProjectAgentsMD(worktreeDir string) string {
	projectPath := filepath.Join(worktreeDir, "AGENTS.md")
	projectContent, err := os.ReadFile(projectPath)
	if err != nil || len(strings.TrimSpace(string(projectContent))) == 0 {
		return ""
	}
	return string(projectContent)
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

// InjectSystemPrompt writes system prompt content to the SYSTEM-PROMPT section
// of AGENTS.override.md at the worktree root.
//
// When replace is true, the system prompt replaces only the SYSTEM-PROMPT
// section — other sections (PROJECT, PERSONA, HOOKS) are preserved. The
// project's checked-in AGENTS.md is written to the PROJECT section if not
// already present.
//
// When replace is false (append), the content is appended to the existing
// SYSTEM-PROMPT section.
//
// Returns the relative path to the written file.
func (a *Adapter) InjectSystemPrompt(worktreeDir, content string, replace bool) (string, error) {
	path := filepath.Join(worktreeDir, "AGENTS.override.md")
	existing, _ := os.ReadFile(path)
	sections := parseSections(string(existing))

	// Ensure project AGENTS.md is in the PROJECT section.
	if _, hasProject := sections[sectionProject]; !hasProject {
		projectContent := readProjectAgentsMD(worktreeDir)
		if projectContent != "" {
			sections[sectionProject] = projectContent
			log.Printf("codex adapter: incorporating project AGENTS.md content into AGENTS.override.md")
		}
	}

	if replace {
		sections[sectionSystemPrompt] = content
	} else {
		// Append to existing SYSTEM-PROMPT section.
		prev := sections[sectionSystemPrompt]
		if prev != "" {
			prev = strings.TrimRight(prev, "\n") + "\n\n"
		}
		sections[sectionSystemPrompt] = prev + content
	}

	rendered := renderSections(sections)
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return "", fmt.Errorf("codex adapter: failed to write AGENTS.override.md: %w", err)
	}
	return "AGENTS.override.md", nil
}

// solGuardRulesFile is the name of the exec policy rules file written by sol
// for guard enforcement. Placed in .codex/rules/ so Codex loads it as part of
// the project-level exec policy.
const solGuardRulesFile = "sol-guards.rules"

// InstallHooks performs best-effort translation of the runtime-agnostic HookSet
// into Codex-native config and AGENTS.override.md instruction text.
//
// Mapping:
//   - Guards       → exec policy deny rules in .codex/rules/sol-guards.rules (real
//     enforcement) AND "IMPORTANT: NEVER run: {pattern}" lines in
//     AGENTS.override.md (defense-in-depth instruction text)
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
		if err := writeProjectConfigBlock(worktreeDir, notifyLine); err != nil {
			log.Printf("codex adapter: failed to write notify to project config: %v", err)
		}
		if len(hooks.TurnBoundary) > 1 {
			log.Printf("codex adapter: WARNING: %d TurnBoundary hooks provided but only the first is used as notify; remaining %d hooks will be instruction text",
				len(hooks.TurnBoundary), len(hooks.TurnBoundary)-1)
		}
	}

	// Translate Guards into exec policy deny rules (real enforcement) and
	// instruction text (defense-in-depth). The exec policy blocks execution
	// at the Codex runtime level; the instruction text discourages the agent
	// from even attempting the command.
	if len(hooks.Guards) > 0 {
		writeGuardRules(worktreeDir, hooks.Guards)
	}

	var instructions []string

	// Translate Guards into instruction text (defense-in-depth).
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

	// Write hook instructions to the HOOKS section of AGENTS.override.md.
	var hookContent strings.Builder
	for _, instr := range instructions {
		hookContent.WriteString(instr)
		hookContent.WriteByte('\n')
	}

	if err := updateSection(worktreeDir, sectionHooks, hookContent.String()); err != nil {
		// Best-effort: log but don't fail.
		log.Printf("codex adapter: failed to write hooks section to AGENTS.override.md: %v", err)
	}
	return nil
}

// writeGuardRules translates Guards into Codex exec policy deny rules and writes
// them to .codex/rules/sol-guards.rules. This provides real enforcement: Codex
// will reject commands matching these prefix rules with decision "forbidden",
// even when running with --dangerously-bypass-approvals-and-sandbox.
//
// Guards that cannot be expressed as exec policy rules (e.g. non-Bash tool
// guards, empty patterns) fall back to instruction-only enforcement.
func writeGuardRules(worktreeDir string, guards []adapter.Guard) {
	rulesDir := filepath.Join(worktreeDir, ".codex", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		log.Printf("codex adapter: failed to create .codex/rules dir: %v (guards will be instruction-only)", err)
		return
	}

	var buf strings.Builder
	buf.WriteString("# Sol guard rules — auto-generated, do not edit.\n")
	buf.WriteString("# These rules block guarded commands at the exec policy level.\n\n")

	var enforced, instructionOnly int
	for _, g := range guards {
		rule, ok := guardToExecPolicyRule(g.Pattern)
		if !ok {
			instructionOnly++
			readable := extractGuardReadable(g.Pattern)
			log.Printf("codex adapter: guard %q → instruction-only (cannot express as exec policy rule)", readable)
			continue
		}
		buf.WriteString(rule)
		buf.WriteByte('\n')
		enforced++
	}

	if enforced == 0 {
		log.Printf("codex adapter: no guards translatable to exec policy rules (%d instruction-only)", instructionOnly)
		return
	}

	rulesPath := filepath.Join(rulesDir, solGuardRulesFile)
	if err := os.WriteFile(rulesPath, []byte(buf.String()), 0o644); err != nil {
		log.Printf("codex adapter: failed to write %s: %v (guards will be instruction-only)", solGuardRulesFile, err)
		return
	}

	log.Printf("codex adapter: wrote %d exec policy deny rules to %s (%d instruction-only)",
		enforced, solGuardRulesFile, instructionOnly)
}

// guardToExecPolicyRule converts a guard pattern into a Starlark exec policy
// prefix_rule with decision="forbidden". Returns the rule string and true if
// the guard can be expressed as an exec policy rule, or ("", false) if not.
//
// Only Bash(...) guards can be translated — other tool guards (e.g.
// "EnterPlanMode") have no command-level equivalent in exec policy.
//
// Examples:
//
//	"Bash(git push --force*)" → `prefix_rule(["git", "push", "--force"], decision="forbidden")`
//	"Bash(rm -rf /*)"         → `prefix_rule(["rm", "-rf", "/"], decision="forbidden")`
//	"EnterPlanMode"           → ("", false) — not a Bash guard
func guardToExecPolicyRule(pattern string) (string, bool) {
	readable := extractGuardReadable(pattern)
	if readable == "" {
		return "", false
	}

	// Only translate Bash(...) guards. Non-Bash tool guards (e.g.
	// "EnterPlanMode", "Write(...)") don't map to shell commands.
	if !strings.HasPrefix(pattern, "Bash(") && strings.Contains(pattern, "(") {
		return "", false
	}
	// Bare patterns without parens (e.g. "EnterPlanMode") are tool-name
	// guards, not command guards.
	if !strings.Contains(pattern, "(") {
		return "", false
	}

	// Split the readable command into tokens for the prefix_rule pattern.
	tokens := strings.Fields(readable)
	if len(tokens) == 0 {
		return "", false
	}

	// Build Starlark prefix_rule: prefix_rule(["tok1", "tok2"], decision="forbidden")
	quoted := make([]string, len(tokens))
	for i, tok := range tokens {
		quoted[i] = fmt.Sprintf("%q", tok)
	}
	rule := fmt.Sprintf("prefix_rule([%s], decision=\"forbidden\")", strings.Join(quoted, ", "))
	return rule, true
}

// Marker comments for the sol-managed block in .codex/config.toml.
const (
	projectConfigBeginMarker = "# BEGIN sol-managed"
	projectConfigEndMarker   = "# END sol-managed"
)

// writeProjectConfigBlock writes content inside a BEGIN/END marker block in
// the project-level .codex/config.toml. If markers already exist, the block is
// replaced; otherwise the block is appended. This makes repeated InstallHooks
// calls idempotent — the file converges to the same content regardless of how
// many times the function is called.
func writeProjectConfigBlock(worktreeDir, content string) error {
	configDir := filepath.Join(worktreeDir, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("codex adapter: failed to create .codex dir: %w", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	existing, _ := os.ReadFile(configPath) // ignore error — file may not exist yet

	block := projectConfigBeginMarker + "\n" + content + projectConfigEndMarker + "\n"

	var updated string
	existingStr := string(existing)
	switch {
	case strings.Contains(existingStr, projectConfigBeginMarker):
		// Replace existing marker block.
		beginIdx := strings.Index(existingStr, projectConfigBeginMarker)
		endIdx := strings.Index(existingStr, projectConfigEndMarker)
		if endIdx == -1 {
			// Malformed: BEGIN without END — replace from BEGIN to EOF.
			updated = existingStr[:beginIdx] + block
		} else {
			after := endIdx + len(projectConfigEndMarker)
			// Skip trailing newline after END marker if present.
			if after < len(existingStr) && existingStr[after] == '\n' {
				after++
			}
			updated = existingStr[:beginIdx] + block + existingStr[after:]
		}
	case len(existingStr) > 0:
		// Append block after existing content.
		if !strings.HasSuffix(existingStr, "\n") {
			existingStr += "\n"
		}
		updated = existingStr + block
	default:
		updated = block
	}

	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
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
// Returns nil if the event is not relevant or has no model information.
//
// Accepted event names (verified from codex-rs/otel/src/metrics/names.rs
// and codex-rs/otel/src/events/shared.rs):
//   - "codex.api_request_initiated" — API call log event
//   - "codex.turn.token_usage"      — token usage metric (histogram)
//   - "codex.sse_event"             — SSE completion event
//
// Attribute keys use Codex's native naming (codex-rs/otel/src/metrics/tags.rs,
// codex-rs/otel/src/events/session_telemetry.rs) with gen_ai.* fallbacks for
// forward compatibility.
func (a *Adapter) ExtractTelemetry(eventName string, attrs map[string]string) *adapter.TelemetryRecord {
	switch eventName {
	case "codex.api_request_initiated", "codex.turn.token_usage", "codex.sse_event":
		// Accepted event names.
	default:
		return nil
	}

	// Extract model — Codex uses "model" (codex-rs/otel/src/events/shared.rs).
	// Fallback to gen_ai.* for forward compatibility.
	model := attrs["model"]
	if model == "" {
		model = attrs["gen_ai.response.model"]
	}
	if model == "" {
		return nil
	}

	// Token counts — Codex uses short names (codex-rs/otel/src/metrics/tags.rs).
	// Fallback to gen_ai.* for forward compatibility.
	input := parseIntAttr(attrs, "input_token_count")
	if input == 0 {
		input = parseIntAttr(attrs, "gen_ai.usage.input_tokens")
	}
	output := parseIntAttr(attrs, "output_token_count")
	if output == 0 {
		output = parseIntAttr(attrs, "gen_ai.usage.output_tokens")
	}

	// Cache read tokens — Codex uses "cached_token_count".
	// Fallback to gen_ai.* for forward compatibility.
	cacheRead := parseIntAttr(attrs, "cached_token_count")
	if cacheRead == 0 {
		cacheRead = parseIntAttr(attrs, "gen_ai.usage.cache_read_input_tokens")
	}

	// Reasoning tokens — Codex-specific (codex-rs/otel/src/metrics/tags.rs).
	// Counted as additional output tokens since TelemetryRecord has no
	// dedicated field.
	reasoning := parseIntAttr(attrs, "reasoning_token_count")
	output += reasoning

	costUSD := parseFloatAttr(attrs, "cost_usd")
	durationMS := parseIntPtrAttr(attrs, "duration_ms")

	return &adapter.TelemetryRecord{
		Model:           model,
		InputTokens:     input,
		OutputTokens:    output,
		CacheReadTokens: cacheRead,
		CostUSD:         costUSD,
		DurationMS:      durationMS,
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
