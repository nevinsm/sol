package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/store"
)

// injectionFileName is the conventional name for the forge injection context file.
const injectionFileName = ".forge-injection.md"

// InjectionConfig holds configuration for building the forge merge injection.
type InjectionConfig struct {
	MaxAttempts    int      // maximum allowed merge attempts
	GateCommands   []string // quality gate commands from world.toml
	WorktreeDir    string   // path to the forge worktree
	AttemptHistory []string // summaries of previous attempts (empty for first attempt)
	TargetBranch   string   // branch to merge into (default: "main")
}

// BuildInjection builds the injection context message for a forge merge session.
// The injection provides the merge session with all the context it needs:
// MR metadata, writ context, attempt history, gate commands, and step-by-step instructions.
// InjectionConfig.TargetBranch must be set by the caller; an empty value is a bug.
func BuildInjection(mr *store.MergeRequest, writ *store.Writ, cfg InjectionConfig) string {
	targetBranch := cfg.TargetBranch

	var b strings.Builder

	// Header with MR metadata.
	b.WriteString("## Merge Task\n\n")
	fmt.Fprintf(&b, "- MR: %s\n", mr.ID)
	fmt.Fprintf(&b, "- Branch: origin/%s\n", mr.Branch)
	fmt.Fprintf(&b, "- Writ: %s (%s)\n", writ.Title, writ.ID)
	fmt.Fprintf(&b, "- Attempt: %d of %d\n", mr.Attempts, cfg.MaxAttempts)
	fmt.Fprintf(&b, "- Target: origin/%s\n", targetBranch)

	// Writ context — gives the merge engineer understanding of the changes.
	b.WriteString("\n### Writ Context\n")
	if writ.Description != "" {
		b.WriteString(writ.Description)
	} else {
		b.WriteString("No description provided.")
	}
	b.WriteString("\n")

	// Previous attempt history — helps the session avoid repeating mistakes.
	b.WriteString("\n### Previous Attempts\n")
	if len(cfg.AttemptHistory) == 0 {
		b.WriteString("First attempt.\n")
	} else {
		for i, summary := range cfg.AttemptHistory {
			fmt.Fprintf(&b, "- Attempt %d: %s\n", i+1, summary)
		}
	}

	// Gate commands — what to run for quality validation.
	b.WriteString("\n### Gate Commands\n")
	if len(cfg.GateCommands) == 0 {
		b.WriteString("No quality gates configured.\n")
	} else {
		for _, cmd := range cfg.GateCommands {
			fmt.Fprintf(&b, "- `%s`\n", cmd)
		}
	}

	// Step-by-step instructions.
	b.WriteString("\n### Instructions\n")
	fmt.Fprintf(&b, "Your worktree is at %s, currently at origin/%s (detached HEAD).\n\n",
		cfg.WorktreeDir, targetBranch)
	fmt.Fprintf(&b, "1. `git fetch origin && git reset --hard origin/%s`\n", targetBranch)
	fmt.Fprintf(&b, "2. `git merge --squash origin/%s`\n", mr.Branch)
	b.WriteString("3. If conflicts, resolve them\n")
	fmt.Fprintf(&b, "4. Commit: `git commit --no-edit -m \"%s (%s)\"`\n", escapeCommitMessage(writ.Title), writ.ID)

	if len(cfg.GateCommands) > 0 {
		gateStr := strings.Join(cfg.GateCommands, " && ")
		fmt.Fprintf(&b, "5. Run gates: `%s`\n", gateStr)
	} else {
		b.WriteString("5. No gates to run\n")
	}

	fmt.Fprintf(&b, "6. If pass: `git push origin HEAD:%s`\n", targetBranch)
	b.WriteString("7. If fail: analyze and report\n")
	b.WriteString("8. Write .forge-result.json with your result\n")

	return b.String()
}

// escapeCommitMessage escapes double quotes in commit messages for safe inclusion
// in the instruction template.
func escapeCommitMessage(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// WriteInjectionFile writes the injection context to .forge-injection.md in the
// given worktree directory. This file serves two purposes:
//   - PrimeBuilder reads it to provide the initial prompt for startup.Launch
//   - PreCompact hook cats it to re-inject context after compaction
//
// Called before startup.Launch for each merge session. Idempotent.
func WriteInjectionFile(worktreeDir, content string) error {
	path := filepath.Join(worktreeDir, injectionFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write injection file %s: %w", path, err)
	}
	return nil
}

// CleanInjectionFile removes the .forge-injection.md file from the worktree.
// Called during session cleanup. Ignores not-found errors (idempotent).
func CleanInjectionFile(worktreeDir string) error {
	path := filepath.Join(worktreeDir, injectionFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clean injection file: %w", err)
	}
	return nil
}
