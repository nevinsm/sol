// Package codex implements runtime.Runtime for the OpenAI Codex agent runtime.
// This is a port of internal/adapter/codex/codex.go to the thin runtime
// contract defined in ADR-0041. The old adapter package is preserved until W5.
package codex

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/runtime/attrutil"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/runtime"
)

// CodexRuntime implements runtime.Runtime for the Codex agent runtime.
type CodexRuntime struct {
	runtime.RuntimeDescriptor
}

// Compile-time interface satisfaction check.
var _ runtime.Runtime = (*CodexRuntime)(nil)

// New returns a new CodexRuntime with descriptor values ported from
// internal/adapter/codex/codex.go.
func New() *CodexRuntime {
	return &CodexRuntime{
		RuntimeDescriptor: runtime.RuntimeDescriptor{
			Name:        "codex",
			PersonaFile: "AGENTS.override.md", // Codex uses AGENTS.override.md for per-agent persona
			SkillsDir:   ".agents/skills",     // Codex discovers skills under .agents/skills/
			ConfigDirEnv:    "CODEX_HOME",
			CredentialFile:  "auth.json",
			GlobalCredsPath: "~/.codex/auth.json",
			DefaultModel:    "gpt-5.4",
			CalloutCommand:  "codex exec", // one-shot invocation; reads prompt from stdin
			SupportedHooks: []string{
				"TurnBoundary", // first hook written as native notify in .codex/config.toml
				"Guard",        // exec policy deny rules in .codex/rules/sol-guards.rules
			},
			StaticEnv: nil,
			CredentialEnvKeys: map[string]string{
				"api_key": "OPENAI_API_KEY",
			},
		},
	}
}

// Descriptor returns the runtime's metadata.
func (r *CodexRuntime) Descriptor() runtime.RuntimeDescriptor {
	return r.RuntimeDescriptor
}

// BuildCommand constructs the codex startup command string.
//
// Format:
//
//	codex [--model <model>] ["<prompt>"]
//
// Approval and sandbox policy are controlled solely via CODEX_HOME/config.toml
// (written by EnsureConfigDir), not CLI flags. The config values
// approval_policy="never" + sandbox_mode="danger-full-access" are equivalent
// to --dangerously-bypass-approvals-and-sandbox, without the conflict risk
// that the CLI flag introduces when explicit config is also present.
//
// If ctx.Continue is true, returns: codex resume --last ["<prompt>"]
// If SOL_SESSION_COMMAND is set (for testing), it is returned verbatim.
func (r *CodexRuntime) BuildCommand(ctx runtime.CommandContext) string {
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

	args := "codex"

	if ctx.Model != "" {
		args += " --model " + ctx.Model
	}

	if ctx.Prompt != "" {
		args += " " + config.ShellQuote(ctx.Prompt)
	}

	return args
}

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
//
// Returns an error if any hook file write fails. Filesystem failures in the
// guard rules, project config, or hook section writes are propagated so
// operators learn when an outpost has started with its configured guards or
// notify hook missing at the enforcement layer.
func (r *CodexRuntime) InstallHooks(worktreeDir string, hooks runtime.HookSet) error {
	// SessionStart hooks run as shell commands at launch — not translatable to
	// agent instructions. Log a warning and skip.
	if len(hooks.SessionStart) > 0 {
		log.Printf("codex runtime: SessionStart hooks are not supported for Codex runtime (%d hooks skipped)", len(hooks.SessionStart))
	}

	// Write the first TurnBoundary hook as a native notify command in the
	// project-level .codex/config.toml. Codex executes this after each turn
	// with a JSON payload — a real hook mechanism.
	if len(hooks.TurnBoundary) > 0 {
		notifyCmd := hooks.TurnBoundary[0].Command
		// Build notify as a TOML array: split command string into argv.
		notifyLine := fmt.Sprintf("notify = %s\n", toTOMLStringArray(strings.Fields(notifyCmd)))
		if err := writeProjectConfigBlock(worktreeDir, notifyLine); err != nil {
			return fmt.Errorf("codex runtime: failed to install notify hook: %w", err)
		}
		// Multi-TurnBoundary degradation: codex only has a single notify
		// slot, so any extra hooks are demoted to instruction text below.
		// This is a known runtime limitation, not a failure — we log a
		// warning but intentionally do NOT return an error. The degradation
		// is functional (the extra hooks still get instruction-text
		// representation) and operators are notified via the log line.
		if len(hooks.TurnBoundary) > 1 {
			log.Printf("codex runtime: WARNING: %d TurnBoundary hooks provided but only the first is used as notify; remaining %d hooks will be instruction text",
				len(hooks.TurnBoundary), len(hooks.TurnBoundary)-1)
		}
	}

	// Translate Guards into exec policy deny rules (real enforcement) and
	// instruction text (defense-in-depth). The exec policy blocks execution
	// at the Codex runtime level; the instruction text discourages the agent
	// from even attempting the command.
	if len(hooks.Guards) > 0 {
		if err := writeGuardRules(worktreeDir, hooks.Guards); err != nil {
			return fmt.Errorf("codex runtime: failed to write guard exec policy rules: %w", err)
		}
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
		return fmt.Errorf("codex runtime: failed to write hooks section: %w", err)
	}
	return nil
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
//
// Seed is a no-op for Codex: it has no pre-launch onboarding screen and its
// per-agent config is written elsewhere (CODEX_HOME/config.toml by other
// machinery). Present to satisfy runtime.Runtime.
func (r *CodexRuntime) Seed(configDir string) error {
	return nil
}

// Attribution context (agent name, world) arrives via X-Sol-* HTTP headers
// configured in CODEX_HOME/config.toml by EnsureConfigDir, then forwarded
// by the ledger's OTLP receiver — not via OTEL_RESOURCE_ATTRIBUTES (which
// Codex does not read at runtime).
func (r *CodexRuntime) ExtractTelemetry(eventName string, attrs map[string]string) *runtime.TelemetryRecord {
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
	input := attrutil.ParseInt(attrs, "input_token_count")
	if input == 0 {
		input = attrutil.ParseInt(attrs, "gen_ai.usage.input_tokens")
	}
	output := attrutil.ParseInt(attrs, "output_token_count")
	if output == 0 {
		output = attrutil.ParseInt(attrs, "gen_ai.usage.output_tokens")
	}

	// Cache read tokens — Codex uses "cached_token_count".
	// Fallback to gen_ai.* for forward compatibility.
	cacheRead := attrutil.ParseInt(attrs, "cached_token_count")
	if cacheRead == 0 {
		cacheRead = attrutil.ParseInt(attrs, "gen_ai.usage.cache_read_input_tokens")
	}

	// Reasoning tokens — Codex-specific (codex-rs/otel/src/metrics/tags.rs).
	reasoning := attrutil.ParseInt(attrs, "reasoning_token_count")

	// Cache creation tokens — match Claude adapter pattern with gen_ai.* fallback.
	cacheCreation := attrutil.ParseInt(attrs, "cache_creation_token_count")
	if cacheCreation == 0 {
		cacheCreation = attrutil.ParseInt(attrs, "gen_ai.usage.cache_creation_input_tokens")
	}

	costUSD := attrutil.ParseFloat(attrs, "cost_usd")
	durationMS := attrutil.ParseIntPtr(attrs, "duration_ms")

	return &runtime.TelemetryRecord{
		Model:               model,
		InputTokens:         input,
		OutputTokens:        output,
		ReasoningTokens:     reasoning,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreation,
		CostUSD:             costUSD,
		DurationMS:          durationMS,
	}
}

// ---- Section management for AGENTS.override.md ----

// Section markers for AGENTS.override.md. Each method writes to its own
// section so that InjectPersona, InjectSystemPrompt, and InstallHooks don't
// clobber each other's content.
const (
	sectionProject      = "SOL:PROJECT"
	sectionPersona      = "SOL:PERSONA"
	sectionSystemPrompt = "SOL:SYSTEM-PROMPT"
	sectionHooks        = "SOL:HOOKS"
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
	if err := fileutil.AtomicWrite(path, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("codex runtime: failed to write AGENTS.override.md: %w", err)
	}
	return nil
}

// ---- Guard rules ----

// solGuardRulesFile is the name of the exec policy rules file written by sol
// for guard enforcement. Placed in .codex/rules/ so Codex loads it as part of
// the project-level exec policy.
const solGuardRulesFile = "sol-guards.rules"

// writeGuardRules translates Guards into Codex exec policy deny rules and writes
// them to .codex/rules/sol-guards.rules. This provides real enforcement: Codex
// will reject commands matching these prefix rules with decision "forbidden",
// even when running with --dangerously-bypass-approvals-and-sandbox.
//
// Guards that cannot be expressed as exec policy rules (e.g. non-Bash tool
// guards, empty patterns) fall back to instruction-only enforcement.
//
// Returns an error if the rules directory or file cannot be written. The
// caller (InstallHooks) propagates this error so operators learn when the
// runtime exec policy enforcement layer fails to install.
func writeGuardRules(worktreeDir string, guards []runtime.Guard) error {
	rulesDir := filepath.Join(worktreeDir, ".codex", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf("codex runtime: failed to create .codex/rules dir: %w", err)
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
			log.Printf("codex runtime: guard %q → instruction-only (cannot express as exec policy rule)", readable)
			continue
		}
		buf.WriteString(rule)
		buf.WriteByte('\n')
		enforced++
	}

	if enforced == 0 {
		log.Printf("codex runtime: no guards translatable to exec policy rules (%d instruction-only)", instructionOnly)
		return nil
	}

	rulesPath := filepath.Join(rulesDir, solGuardRulesFile)
	if err := fileutil.AtomicWrite(rulesPath, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("codex runtime: failed to write %s: %w", solGuardRulesFile, err)
	}

	log.Printf("codex runtime: wrote %d exec policy deny rules to %s (%d instruction-only)",
		enforced, solGuardRulesFile, instructionOnly)
	return nil
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

// ---- Project config block management ----

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
		return fmt.Errorf("codex runtime: failed to create .codex dir: %w", err)
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

	if err := fileutil.AtomicWrite(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("codex runtime: failed to write .codex/config.toml: %w", err)
	}
	return nil
}

// ---- String helpers ----

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
