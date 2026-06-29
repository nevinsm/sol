package runtime

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/fileutil"
)

// solManagedMarker is the filename placed inside sol-generated skill directories
// to distinguish them from custom project skills. Only directories containing
// this marker are candidates for stale-skill removal.
const solManagedMarker = ".sol-managed"

// WritePersonaFile writes raw persona content to <worktreeDir>/<d.PersonaFile>.
// Shared helper used by runtimes whose persona file is a standalone file
// (claude). Section-aware runtimes (codex) bypass this and write directly
// via their own logic.
func WritePersonaFile(d RuntimeDescriptor, worktreeDir string, content []byte) error {
	path := filepath.Join(worktreeDir, d.PersonaFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("runtime %s: failed to create persona directory: %w", d.Name, err)
	}
	if err := fileutil.AtomicWrite(path, content, 0o644); err != nil {
		return fmt.Errorf("runtime %s: failed to write persona file %q: %w", d.Name, d.PersonaFile, err)
	}
	return nil
}

// InstallSkills writes skills to <worktreeDir>/<d.SkillsDir>, removing stale ones.
// Each skill is written to <skillsDir>/<name>/SKILL.md and marked as sol-managed.
// Sol-managed directories present on disk but not in the skills list are removed.
// Non-sol directories (those without a .sol-managed marker) are preserved.
//
// Skills are written before stale directories are removed so that a write
// failure (e.g. disk full) leaves the previous skills intact.
func InstallSkills(d RuntimeDescriptor, worktreeDir string, skills []Skill) error {
	skillsDir := filepath.Join(worktreeDir, d.SkillsDir)
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("runtime %s: failed to create skills directory: %w", d.Name, err)
	}

	// Build set of incoming skill names for stale-removal phase.
	current := make(map[string]bool, len(skills))
	for _, s := range skills {
		current[s.Name] = true
	}

	// Write each skill atomically — partial writes won't corrupt existing files.
	for _, s := range skills {
		skillDir := filepath.Join(skillsDir, s.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("runtime %s: failed to create skill dir %q: %w", d.Name, s.Name, err)
		}
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := fileutil.AtomicWrite(skillPath, []byte(s.Content), 0o644); err != nil {
			return fmt.Errorf("runtime %s: failed to write skill %q: %w", d.Name, s.Name, err)
		}
		// Mark directory as sol-managed so stale cleanup can identify it.
		markerPath := filepath.Join(skillDir, solManagedMarker)
		if err := fileutil.AtomicWrite(markerPath, nil, 0o644); err != nil {
			return fmt.Errorf("runtime %s: failed to write sol-managed marker for skill %q: %w", d.Name, s.Name, err)
		}
	}

	// Remove stale sol-managed skill directories.
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return fmt.Errorf("runtime %s: failed to read skills directory: %w", d.Name, err)
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
			return fmt.Errorf("runtime %s: failed to remove stale skill %q: %w", d.Name, e.Name(), err)
		}
	}

	return nil
}

// InjectSystemPrompt writes the system prompt to a known relative path under
// worktreeDir. The path is derived from d.SkillsDir: the parent directory of
// SkillsDir is used as the base, and the file is named "system-prompt.md".
//
// For example, if d.SkillsDir is ".claude/skills", the prompt is written to
// ".claude/system-prompt.md" and that relative path is returned.
//
// When replace is false the content is appended to any existing content.
// When replace is true the file is overwritten.
//
// Returns the relative path written, for use in BuildCommand reference.
func InjectSystemPrompt(d RuntimeDescriptor, worktreeDir, content string, replace bool) (string, error) {
	// Derive the base directory from SkillsDir's parent.
	baseDir := filepath.ToSlash(filepath.Dir(d.SkillsDir))
	var relPath string
	if baseDir == "" || baseDir == "." {
		relPath = "system-prompt.md"
	} else {
		relPath = baseDir + "/system-prompt.md"
	}

	absDir := filepath.Join(worktreeDir, filepath.FromSlash(baseDir))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("runtime %s: failed to create directory for system prompt: %w", d.Name, err)
	}

	promptPath := filepath.Join(worktreeDir, filepath.FromSlash(relPath))

	var data []byte
	if replace {
		data = []byte(content)
	} else {
		// Append: preserve existing content and add new content after two newlines.
		existing, _ := os.ReadFile(promptPath) // ignore ENOENT
		if len(strings.TrimSpace(string(existing))) > 0 {
			trimmed := strings.TrimRight(string(existing), "\n")
			data = []byte(trimmed + "\n\n" + content)
		} else {
			data = []byte(content)
		}
	}

	if err := fileutil.AtomicWrite(promptPath, data, 0o644); err != nil {
		return "", fmt.Errorf("runtime %s: failed to write system prompt: %w", d.Name, err)
	}

	return relPath, nil
}

// EnsureConfigDir creates the per-agent config directory at
// <worldDir>/.<d.Name>-config/<roleDir>/<agent>/ (where roleDir maps
// "envoy"→"envoys", "outpost"→"outposts", else passthrough), optionally creates
// a symlink from <configDir>/<d.CredentialFile> pointing at d.GlobalCredsPath
// (expanded at use time), and returns a ConfigResult with d.ConfigDirEnv set to
// configDir.
//
// Credential symlink behavior (when d.CredentialFile and d.GlobalCredsPath are
// both set):
//   - Any pre-existing credential file or symlink is always removed first
//     (idempotent cleanup; removes stale symlinks from persistent config dirs).
//   - The symlink is then created ONLY when no credential env var is present in
//     env. If any key in d.CredentialEnvKeys maps to a non-empty value in env,
//     the env var is the sole authoritative credential and no on-disk symlink is
//     written. This prevents the on-disk subscription credential from competing
//     with (or expiring beneath) an operator-configured env credential.
//
// If GlobalCredsPath expands to a file that does not exist, the symlink will
// be dangling — the runtime will report its own authentication error.
//
// Idempotent: safe to call repeatedly for the same agent.
func EnsureConfigDir(d RuntimeDescriptor, worldDir, role, agent string, env map[string]string) (ConfigResult, error) {
	configDir := filepath.Join(worldDir, "."+d.Name+"-config", roleDir(role), agent)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return ConfigResult{}, fmt.Errorf("runtime %s: failed to create config dir %q: %w", d.Name, configDir, err)
	}

	// Manage credential symlink when both fields are configured.
	if d.CredentialFile != "" && d.GlobalCredsPath != "" {
		credLink := filepath.Join(configDir, d.CredentialFile)
		os.Remove(credLink) // always clear pre-existing link/file (idempotent cleanup)
		if !credentialEnvConfigured(d, env) {
			globalCreds, err := expandPath(d.GlobalCredsPath)
			if err != nil {
				return ConfigResult{}, fmt.Errorf("runtime %s: failed to expand global creds path: %w", d.Name, err)
			}
			if err := os.Symlink(globalCreds, credLink); err != nil {
				return ConfigResult{}, fmt.Errorf("runtime %s: failed to create credential symlink: %w", d.Name, err)
			}
		}
	}

	envVars := make(map[string]string)
	if d.ConfigDirEnv != "" {
		envVars[d.ConfigDirEnv] = configDir
	}

	return ConfigResult{
		Dir:    configDir,
		EnvVar: envVars,
	}, nil
}

// CleanupConfigDir removes per-agent config state created by EnsureConfigDir.
// Path uses the same roleDir mapping as EnsureConfigDir ("envoy"→"envoys",
// "outpost"→"outposts", else passthrough).
// Outposts only — caller MUST NOT invoke for envoys or forge (their config is
// durable). Idempotent: returns nil if the directory does not exist.
func CleanupConfigDir(d RuntimeDescriptor, worldDir, role, agent string) error {
	configDir := filepath.Join(worldDir, "."+d.Name+"-config", roleDir(role), agent)
	if err := os.RemoveAll(configDir); err != nil {
		return fmt.Errorf("runtime %s: failed to remove config dir %q: %w", d.Name, configDir, err)
	}
	return nil
}

// BuildTelemetryEnv returns OTLP env vars for the agent session.
// Returns an empty map if port <= 0 (telemetry disabled).
//
// Sets OTEL_RESOURCE_ATTRIBUTES with agent.name, world, optional writ_id,
// optional account, and service.name derived from d.Name. Also includes
// standard OTLP HTTP exporter vars pointing at the ledger endpoint.
// Any vars in d.StaticEnv are merged in (only when port > 0).
func BuildTelemetryEnv(d RuntimeDescriptor, port int, agent, world, writID, account string) map[string]string {
	if port <= 0 {
		return map[string]string{}
	}

	// Build OTEL_RESOURCE_ATTRIBUTES.
	attrs := fmt.Sprintf("agent.name=%s,world=%s", agent, world)
	if writID != "" {
		attrs += ",writ_id=" + writID
	}
	if account != "" {
		attrs += ",account=" + account
	}
	attrs += ",service.name=" + d.Name

	env := map[string]string{
		"OTEL_LOGS_EXPORTER":               "otlp",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": fmt.Sprintf("http://localhost:%d/v1/logs", port),
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL": "http/json",
		"OTEL_RESOURCE_ATTRIBUTES":         attrs,
	}

	// Merge runtime-specific constants (e.g. CLAUDE_CODE_ENABLE_TELEMETRY=1).
	maps.Copy(env, d.StaticEnv)

	return env
}

// CredentialEnv returns env vars for the given credential.
// Uses d.CredentialEnvKeys to map credential types to env var names.
// Returns an error if the credential type is not mapped for this runtime.
func CredentialEnv(d RuntimeDescriptor, cred Credential) (map[string]string, error) {
	if len(d.CredentialEnvKeys) == 0 {
		return nil, fmt.Errorf("runtime %s: no credential env key mapping configured", d.Name)
	}
	envKey, ok := d.CredentialEnvKeys[cred.Type]
	if !ok {
		return nil, fmt.Errorf("unrecognized credential type %q for runtime %s — no credentials set; session will fail authentication", cred.Type, d.Name)
	}
	return map[string]string{envKey: cred.Token}, nil
}

// MemoryDir returns the absolute path to the per-agent memory directory.
// Runtime-agnostic — sol owns the path scheme:
// <worldDir>/<roleDir>/<agent>/memory/ where roleDir maps
// "envoy"→"envoys", "outpost"→"outposts", else passthrough.
// Returns "" if any required argument is empty or if the path cannot be made absolute.
func MemoryDir(worldDir, role, agent string) string {
	if worldDir == "" || role == "" || agent == "" {
		return ""
	}
	dir := filepath.Join(worldDir, roleDir(role), agent, "memory")
	if filepath.IsAbs(dir) {
		return dir
	}
	// Defensive: promote relative paths to absolute. This should only happen
	// if the caller passes a relative worldDir. Callers depend on the returned
	// path being absolute (e.g. Claude Code silently ignores relative
	// autoMemoryDirectory values).
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return ""
}

// roleDir returns the directory name for the given role under per-agent path
// layouts. Mirrors the pre-ADR-0041 mapping used by config.ClaudeConfigDir:
//   - "envoy"   → "envoys"
//   - "outpost" → "outposts"
//   - anything else (e.g. "forge") → role as-is
func roleDir(role string) string {
	switch role {
	case "envoy":
		return "envoys"
	case "outpost":
		return "outposts"
	default:
		return role
	}
}

// credentialEnvConfigured reports whether any credential env var listed in
// d.CredentialEnvKeys has a non-empty value in env. When true, the operator has
// configured an env-var credential and EnsureConfigDir must not plant a competing
// on-disk credential symlink.
func credentialEnvConfigured(d RuntimeDescriptor, env map[string]string) bool {
	for _, envKey := range d.CredentialEnvKeys {
		if env[envKey] != "" {
			return true
		}
	}
	return false
}

// expandPath expands a leading "~/" to the user's home directory.
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to determine home directory: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
