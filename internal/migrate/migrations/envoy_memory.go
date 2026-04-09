// envoy_memory.go registers the envoy-memory migration that shifts envoy
// persistent memory from sol's legacy .brief/memory.md scheme to Claude
// Code's native auto-memory via the autoMemoryDirectory setting.
//
// Phase 1 of the envoy-memory-migration caravan. The migration is registered
// here and consumed by the sol migrate framework in the parent package. A
// later caravan phase retires the internal/brief package entirely; during
// the migration window the two systems coexist.
package migrations

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nevinsm/sol/internal/adapter"
	_ "github.com/nevinsm/sol/internal/adapter/claude" // register claude adapter
	_ "github.com/nevinsm/sol/internal/adapter/codex"  // register codex adapter
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
)

func init() {
	migrate.Register(migrate.Migration{
		Name:    "envoy-memory",
		Version: "0.2.0",
		Title:   "Envoy briefs → Claude auto-memory",
		Description: `Shifts envoy persistent memory from the legacy
.brief/memory.md system to Claude Code's native auto-memory via the
autoMemoryDirectory setting.

For each envoy detected with a .brief/ directory:
  1. Copy all files in <worktree>/.brief/ to <envoyDir>/memory/
  2. Rename memory.md → MEMORY.md (Claude Code's expected index name)
  3. Append a "Migrated Topic Notes" section to MEMORY.md with links to
     the migrated topic files (broker-abstraction.md, etc.)
  4. Preserve non-.md files verbatim for audit trail
  5. Delete the source .brief/ directory after a confirmed successful copy
  6. Remove any empty <envoyDir>/.brief/ legacy placeholder outside the
     worktree

Codex envoys are skipped with a clear "codex has no memory system" message —
codex's MemoryDir() contract is empty, and this migration refuses to invent
a destination directory where the runtime cannot consume it.

Run this migration BEFORE upgrading to the release that removes the brief
inject hook. The migration is idempotent and per-envoy — safe to re-run;
envoys whose target memory directory already has MEMORY.md are skipped.

BACKUPS: the migration deletes .brief/ after a successful copy. Operators
who want a pre-migration snapshot should archive their worlds' .brief/
directories manually (e.g. tar) before running.

Preconditions:
  - No in-flight handoff operations
  - Sphere may be up or down (sphere.db is accessed via the shared store)
`,
		Detect: detectEnvoyMemory,
		Run:    runEnvoyMemory,
	})
}

// envoyMemoryTarget captures the paths a single envoy needs migrated. It is
// computed once by detect/run helpers so the two code paths agree on which
// files are in scope.
type envoyMemoryTarget struct {
	World       string
	Agent       string
	WorktreeDir string // <worldDir>/envoys/<agent>/worktree
	BriefDir    string // <worktreeDir>/.brief
	LegacyDir   string // <worldDir>/envoys/<agent>/.brief (legacy placeholder)
	MemoryDir   string // destination, from adapter.MemoryDir
	Runtime     string // resolved runtime name ("claude", "codex", ...)
}

// detectEnvoyMemory walks every registered world's envoys and counts the
// ones with brief content waiting to be migrated. Codex envoys are
// intentionally not counted — they have no destination memory directory.
func detectEnvoyMemory(ctx migrate.Context) (migrate.DetectResult, error) {
	if ctx.SphereStore == nil {
		return migrate.DetectResult{Reason: "sphere store unavailable"}, nil
	}
	worlds, err := ctx.SphereStore.ListWorlds()
	if err != nil {
		return migrate.DetectResult{}, fmt.Errorf("envoy-memory: list worlds: %w", err)
	}

	var (
		envoysWithBrief int
		legacyDirs      int
		affectedWorlds  = map[string]bool{}
	)
	for _, w := range worlds {
		envoys, err := ctx.SphereStore.ListAgents(w.Name, "")
		if err != nil {
			return migrate.DetectResult{}, fmt.Errorf("envoy-memory: list agents in %q: %w", w.Name, err)
		}
		for _, a := range envoys {
			if a.Role != "envoy" {
				continue
			}
			tgt := buildTarget(w.Name, a.Name)
			briefHasContent, err := dirHasFile(tgt.BriefDir)
			if err != nil {
				return migrate.DetectResult{}, fmt.Errorf("envoy-memory: scan %q: %w", tgt.BriefDir, err)
			}
			legacyExists := pathExists(tgt.LegacyDir)
			if briefHasContent {
				envoysWithBrief++
				affectedWorlds[w.Name] = true
			}
			if legacyExists {
				legacyDirs++
				affectedWorlds[w.Name] = true
			}
		}
	}

	needed := envoysWithBrief > 0 || legacyDirs > 0
	reason := ""
	switch {
	case needed && envoysWithBrief > 0 && legacyDirs > 0:
		reason = fmt.Sprintf("%d envoys with .brief content, %d legacy placeholder dirs", envoysWithBrief, legacyDirs)
	case needed && envoysWithBrief > 0:
		reason = fmt.Sprintf("%d envoys with .brief content across %d worlds", envoysWithBrief, len(affectedWorlds))
	case needed && legacyDirs > 0:
		reason = fmt.Sprintf("%d legacy placeholder dirs to remove", legacyDirs)
	default:
		reason = "no envoys with brief content detected"
	}
	return migrate.DetectResult{Needed: needed, Reason: reason}, nil
}

// runEnvoyMemory executes the migration for every applicable envoy. It is
// idempotent: envoys whose target memory directory already has MEMORY.md are
// left alone unless opts.Force is set.
func runEnvoyMemory(ctx migrate.Context, opts migrate.RunOpts) (migrate.RunResult, error) {
	if ctx.SphereStore == nil {
		return migrate.RunResult{}, fmt.Errorf("envoy-memory: sphere store unavailable")
	}
	worlds, err := ctx.SphereStore.ListWorlds()
	if err != nil {
		return migrate.RunResult{}, fmt.Errorf("envoy-memory: list worlds: %w", err)
	}

	type agentStatus struct {
		Agent   string `json:"agent"`
		Status  string `json:"status"` // "migrated", "skipped", "error", "codex-skipped"
		Message string `json:"message,omitempty"`
	}
	perWorld := map[string][]agentStatus{}
	var (
		migrated int
		skipped  int
		errored  int
	)

	for _, w := range worlds {
		if opts.World != "" && opts.World != w.Name {
			continue
		}
		envoys, err := ctx.SphereStore.ListAgents(w.Name, "")
		if err != nil {
			return migrate.RunResult{}, fmt.Errorf("envoy-memory: list agents in %q: %w", w.Name, err)
		}
		for _, a := range envoys {
			if a.Role != "envoy" {
				continue
			}
			tgt := buildTarget(w.Name, a.Name)
			briefHasContent, scanErr := dirHasFile(tgt.BriefDir)
			if scanErr != nil {
				errored++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "error", Message: scanErr.Error()})
				continue
			}
			legacyExists := pathExists(tgt.LegacyDir)
			if !briefHasContent && !legacyExists {
				continue
			}

			// Resolve runtime for this envoy to decide whether a memory
			// destination exists. Codex has none; skip cleanly.
			runtime, rtErr := resolveEnvoyRuntime(w.Name)
			if rtErr != nil {
				errored++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "error", Message: rtErr.Error()})
				continue
			}
			tgt.Runtime = runtime

			rt, ok := adapter.Get(runtime)
			if !ok {
				errored++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "error", Message: fmt.Sprintf("runtime %q not registered", runtime)})
				continue
			}
			tgt.MemoryDir = rt.MemoryDir(config.WorldDir(w.Name), "envoy", a.Name)
			if tgt.MemoryDir == "" {
				skipped++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{
					Agent:   a.Name,
					Status:  "codex-skipped",
					Message: fmt.Sprintf("%s envoy skipped: no memory system", runtime),
				})
				// Still remove the empty legacy placeholder if present —
				// it's dead state regardless of runtime.
				if legacyExists {
					_ = removeEmptyDir(tgt.LegacyDir)
				}
				continue
			}
			if !filepath.IsAbs(tgt.MemoryDir) {
				// Should be impossible given the adapter's pinned test, but
				// defend the invariant at the migration boundary too.
				errored++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "error", Message: fmt.Sprintf("adapter returned relative memoryDir %q", tgt.MemoryDir)})
				continue
			}

			// Idempotency: skip if target already has MEMORY.md and Force
			// is not set. Legacy placeholder cleanup still runs so re-runs
			// converge.
			if fileExists(filepath.Join(tgt.MemoryDir, "MEMORY.md")) && !opts.Force {
				skipped++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "skipped", Message: "already migrated"})
				if legacyExists {
					_ = removeEmptyDir(tgt.LegacyDir)
				}
				continue
			}

			// If there is only a legacy placeholder and no brief content,
			// just remove the placeholder and move on.
			if !briefHasContent && legacyExists {
				if err := removeEmptyDir(tgt.LegacyDir); err != nil {
					errored++
					perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "error", Message: err.Error()})
					continue
				}
				migrated++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "migrated", Message: "legacy placeholder removed"})
				continue
			}

			if err := migrateSingleEnvoy(tgt); err != nil {
				errored++
				perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "error", Message: err.Error()})
				continue
			}
			migrated++
			perWorld[w.Name] = append(perWorld[w.Name], agentStatus{Agent: a.Name, Status: "migrated"})
		}
	}

	summary := fmt.Sprintf("migrated %d envoys across %d worlds (%d skipped, %d errored)", migrated, len(perWorld), skipped, errored)
	details := map[string]any{
		"migrated": migrated,
		"skipped":  skipped,
		"errored":  errored,
		"per_world": func() map[string]any {
			out := map[string]any{}
			for w, items := range perWorld {
				out[w] = items
			}
			return out
		}(),
	}
	if errored > 0 {
		return migrate.RunResult{Summary: summary, Details: details}, fmt.Errorf("envoy-memory: %d envoys failed to migrate (see details)", errored)
	}
	return migrate.RunResult{Summary: summary, Details: details}, nil
}

// migrateSingleEnvoy does the per-envoy filesystem work: copy .brief/ →
// memory/, rename memory.md → MEMORY.md, append the Migrated Topic Notes
// section, and clean up the source on success.
func migrateSingleEnvoy(tgt envoyMemoryTarget) error {
	if err := os.MkdirAll(tgt.MemoryDir, 0o755); err != nil {
		return fmt.Errorf("create memory dir %q: %w", tgt.MemoryDir, err)
	}

	entries, err := os.ReadDir(tgt.BriefDir)
	if err != nil {
		return fmt.Errorf("read brief dir %q: %w", tgt.BriefDir, err)
	}

	var topicFiles []string
	for _, e := range entries {
		if e.IsDir() {
			// Brief dirs are flat in practice; preserve any nested dirs by
			// a recursive copy so we don't silently lose audit state.
			if err := copyTree(filepath.Join(tgt.BriefDir, e.Name()), filepath.Join(tgt.MemoryDir, e.Name())); err != nil {
				return fmt.Errorf("copy subdir %q: %w", e.Name(), err)
			}
			continue
		}
		src := filepath.Join(tgt.BriefDir, e.Name())
		dstName := e.Name()
		if e.Name() == "memory.md" {
			dstName = "MEMORY.md"
		}
		dst := filepath.Join(tgt.MemoryDir, dstName)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy %q: %w", e.Name(), err)
		}
		// Track topic files (non-index .md files that aren't the index)
		if strings.HasSuffix(e.Name(), ".md") && e.Name() != "memory.md" {
			topicFiles = append(topicFiles, dstName)
		}
	}
	sort.Strings(topicFiles)

	// Append the Migrated Topic Notes section to MEMORY.md. If there is no
	// MEMORY.md yet (a brief with no memory.md), create one with the section
	// as the entire file.
	if err := appendMigratedTopicNotes(filepath.Join(tgt.MemoryDir, "MEMORY.md"), topicFiles); err != nil {
		return fmt.Errorf("append topic notes: %w", err)
	}

	// Delete source brief after successful copy — operators who wanted a
	// backup took one before running the migration (see Description).
	if err := os.RemoveAll(tgt.BriefDir); err != nil {
		return fmt.Errorf("remove brief dir %q: %w", tgt.BriefDir, err)
	}
	// Best-effort remove of the legacy placeholder at the envoy root.
	if pathExists(tgt.LegacyDir) {
		_ = removeEmptyDir(tgt.LegacyDir)
	}
	return nil
}

// appendMigratedTopicNotes appends (or creates) the "Migrated Topic Notes"
// section at the end of the MEMORY.md index. The section is a bullet list of
// relative links to the sibling topic files that were migrated alongside
// memory.md. If there were no topic files, the section is still appended
// (empty-body marker) so operators have a breadcrumb that a migration ran.
func appendMigratedTopicNotes(memoryPath string, topics []string) error {
	var existing []byte
	if data, err := os.ReadFile(memoryPath); err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return err
	}

	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("## Migrated Topic Notes\n\n")
	if len(topics) == 0 {
		buf.WriteString("_(no sibling topic notes were migrated)_\n")
	} else {
		for _, t := range topics {
			title := strings.TrimSuffix(t, ".md")
			fmt.Fprintf(&buf, "- [%s](./%s)\n", title, t)
		}
	}

	tmp := memoryPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, memoryPath)
}

// buildTarget returns the canonical paths for an envoy's migration state.
// Centralizing this keeps detect and run in lock-step.
func buildTarget(world, agent string) envoyMemoryTarget {
	envoyDir := filepath.Join(config.WorldDir(world), "envoys", agent)
	return envoyMemoryTarget{
		World:       world,
		Agent:       agent,
		WorktreeDir: filepath.Join(envoyDir, "worktree"),
		BriefDir:    filepath.Join(envoyDir, "worktree", ".brief"),
		LegacyDir:   filepath.Join(envoyDir, ".brief"),
	}
}

// resolveEnvoyRuntime returns the runtime name ("claude", "codex") for the
// envoy role in the given world. Defaults to "claude" on any load error so
// migration behavior is consistent with sol's sphere-wide default.
func resolveEnvoyRuntime(world string) (string, error) {
	cfg, err := config.LoadWorldConfig(world)
	if err != nil {
		// Surface the error so operators can diagnose misconfigured worlds,
		// but fall back to claude — the historical default — so a missing
		// world.toml does not block migration.
		return "claude", nil //nolint:nilerr // intentional soft fallback
	}
	return cfg.ResolveRuntime("envoy"), nil
}

// dirHasFile reports whether the given directory contains at least one
// regular file (recursively). Non-existent directories return (false, nil).
func dirHasFile(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	var found bool
	err = filepath.Walk(dir, func(_ string, fi os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if fi.Mode().IsRegular() {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil && !found {
		return false, err
	}
	return found, nil
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// removeEmptyDir removes a directory only if it has no entries. Used to tidy
// up the legacy <envoyDir>/.brief placeholder without nuking anything the
// operator may have dropped there intentionally.
func removeEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) != 0 {
		return nil // not empty; leave alone
	}
	return os.Remove(dir)
}

// copyFile copies src to dst, preserving contents byte-for-byte. Parent
// directory must already exist.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyTree recursively copies a directory tree. Used as a defensive fallback
// for any nested subdirectories the operator may have placed under .brief/.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode().IsRegular() {
			return copyFile(path, target)
		}
		return nil
	})
}

// Compile-time check: the sphere store methods we use exist. This keeps a
// future interface rename from silently breaking the migration at runtime.
var _ interface {
	ListWorlds() ([]store.World, error)
	ListAgents(world, state string) ([]store.Agent, error)
} = (*store.SphereStore)(nil)
