package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var guardCmd = &cobra.Command{
	Use:     "guard",
	Short:   "Block forbidden operations (PreToolUse hook)",
	GroupID: groupPlumbing,
	Long: `Block forbidden operations via Claude Code PreToolUse hooks.

Guard commands exit with code 2 to BLOCK tool execution when a policy
is violated. They're called before the tool runs, preventing the
forbidden operation entirely.

Available guards:
  dangerous-command  - Block rm -rf /, force push, hard reset, git clean, checkout --
  workflow-bypass    - Block PR creation, direct push to main, manual branching

Example hook configuration:
  {
    "PreToolUse": [{
      "matcher": "Bash(git push --force*)",
      "hooks": [{"command": "sol guard dangerous-command"}]
    }]
  }`,
}

// --- dangerous-command ---

var guardDangerousCmd = &cobra.Command{
	Use:          "dangerous-command",
	Short:        "Block dangerous commands (rm -rf, force push, hard reset, etc.)",
	SilenceUsage: true,
	Long: `Block dangerous commands via Claude Code PreToolUse hooks.

This guard blocks operations that could cause irreversible damage:
  - git push --force/-f  (--force-with-lease and --force-if-includes are allowed)
  - git reset --hard
  - git clean -f / git clean -fd
  - git checkout -- . / git restore .
  - rm -rf /

The guard reads the tool input from stdin (Claude Code hook protocol)
and exits with code 2 to block dangerous operations.

Exit codes:
  0 - Operation allowed
  2 - Operation BLOCKED`,
	RunE: runGuardDangerous,
}

// --- workflow-bypass ---

var guardWorkflowBypassCmd = &cobra.Command{
	Use:          "workflow-bypass",
	Short:        "Block commands that circumvent the forge merge pipeline",
	SilenceUsage: true,
	Long: `Block workflow-bypass operations via Claude Code PreToolUse hooks.

This guard blocks commands that circumvent Sol's forge merge pipeline:
  - git push origin main/master  (agents must use sol resolve → forge)
  - gh pr create                 (Sol uses its own MR system)
  - git checkout -b / git switch -c  (outposts have their branch assigned)

Role exemptions:
  Forge (SOL_ROLE=forge) is exempt since it needs to push to the target
  branch for merges.

Exit codes:
  0 - Operation allowed
  2 - Operation BLOCKED`,
	RunE: runGuardWorkflowBypass,
}

func init() {
	rootCmd.AddCommand(guardCmd)
	guardCmd.AddCommand(guardDangerousCmd)
	guardCmd.AddCommand(guardWorkflowBypassCmd)
}

// --- dangerous-command implementation ---

// dangerousPattern defines a pattern to match via substring containment.
type dangerousPattern struct {
	contains []string
	reason   string
}

// fragmentPatterns use simple containment matching (all substrings must appear).
var fragmentPatterns = []dangerousPattern{
	{[]string{"git", "reset", "--hard"}, "Hard reset discards all uncommitted changes irreversibly"},
	{[]string{"git", "clean", "-f"}, "git clean -f deletes untracked files irreversibly"},
}

// safeForceFlags are git push flags that look like --force but are safe.
var safeForceFlags = []string{"--force-with-lease", "--force-if-includes"}

func runGuardDangerous(cmd *cobra.Command, args []string) error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail open
	}

	command := guardExtractCommand(input)
	if command == "" {
		return nil
	}

	lower := strings.ToLower(command)

	// Check special patterns that need smarter matching
	if reason := matchDangerousRmRf(lower); reason != "" {
		printGuardBlock("DANGEROUS COMMAND BLOCKED", reason, command)
		return &exitError{code: 2}
	}
	if reason := matchDangerousGitPush(lower); reason != "" {
		printGuardBlock("DANGEROUS COMMAND BLOCKED", reason, command)
		return &exitError{code: 2}
	}
	if reason := matchDangerousCheckoutRestore(lower); reason != "" {
		printGuardBlock("DANGEROUS COMMAND BLOCKED", reason, command)
		return &exitError{code: 2}
	}

	// Check simple fragment patterns
	for _, pattern := range fragmentPatterns {
		if matchAllFragments(lower, pattern.contains) {
			printGuardBlock("DANGEROUS COMMAND BLOCKED", pattern.reason, command)
			return &exitError{code: 2}
		}
	}

	return nil
}

// --- workflow-bypass implementation ---

// workflowBypassPattern defines a workflow-bypass pattern.
type workflowBypassPattern struct {
	contains []string
	reason   string
}

var workflowBypassPatterns = []workflowBypassPattern{
	{[]string{"gh", "pr", "create"}, "Sol uses its own MR system — use sol resolve, not GitHub PRs"},
}

func runGuardWorkflowBypass(cmd *cobra.Command, args []string) error {
	// Forge is exempt — it needs to push to the target branch for merges.
	if strings.EqualFold(os.Getenv("SOL_ROLE"), "forge") {
		return nil
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail open
	}

	command := guardExtractCommand(input)
	if command == "" {
		return nil
	}

	lower := strings.ToLower(command)

	// Check push to main/master
	if reason := matchPushToProtectedBranch(lower); reason != "" {
		printGuardBlock("WORKFLOW BYPASS BLOCKED", reason, command)
		return &exitError{code: 2}
	}

	// Check manual branching
	if reason := matchManualBranching(lower); reason != "" {
		printGuardBlock("WORKFLOW BYPASS BLOCKED", reason, command)
		return &exitError{code: 2}
	}

	// Check simple patterns (gh pr create)
	for _, pattern := range workflowBypassPatterns {
		if matchAllFragments(lower, pattern.contains) {
			printGuardBlock("WORKFLOW BYPASS BLOCKED", pattern.reason, command)
			return &exitError{code: 2}
		}
	}

	return nil
}

// --- matching helpers ---

// guardExtractCommand extracts the bash command from Claude Code hook input JSON.
func guardExtractCommand(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var hookInput struct {
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(input, &hookInput); err != nil {
		return ""
	}
	return hookInput.ToolInput.Command
}

// matchAllFragments returns true if all fragments appear in the command.
func matchAllFragments(command string, fragments []string) bool {
	for _, f := range fragments {
		if !strings.Contains(command, strings.ToLower(f)) {
			return false
		}
	}
	return true
}

// matchDangerousRmRf blocks "rm -rf /" targeting the root filesystem.
func matchDangerousRmRf(command string) string {
	if !strings.Contains(command, "rm") {
		return ""
	}
	fields := strings.Fields(command)
	hasRm := false
	hasRecursiveForce := false
	for _, f := range fields {
		if f == "rm" {
			hasRm = true
		}
		if strings.HasPrefix(f, "-") && strings.Contains(f, "r") && strings.Contains(f, "f") {
			hasRecursiveForce = true
		}
		if hasRm && hasRecursiveForce && (f == "/" || f == "/*") {
			return "Filesystem destruction (rm -rf /)"
		}
	}
	return ""
}

// matchDangerousGitPush blocks "git push --force/-f" while allowing safe
// variants like "--force-with-lease" and "--force-if-includes".
func matchDangerousGitPush(command string) string {
	if !strings.Contains(command, "git") || !strings.Contains(command, "push") {
		return ""
	}
	fields := strings.Fields(command)
	hasPush := false
	for i, f := range fields {
		if f == "push" && i > 0 && fields[i-1] == "git" {
			hasPush = true
			continue
		}
		if !hasPush {
			continue
		}
		if f == "--force" || f == "-f" {
			return "Force push rewrites remote history and can destroy others' work"
		}
		// Safe force variants are allowed — no need to check further for them
	}
	return ""
}

// matchDangerousCheckoutRestore blocks "git checkout -- ." and "git restore ."
// which discard all uncommitted changes.
func matchDangerousCheckoutRestore(command string) string {
	if !strings.Contains(command, "git") {
		return ""
	}
	fields := strings.Fields(command)
	// Look for "git checkout -- ." pattern
	for i := 0; i+3 < len(fields); i++ {
		if fields[i] == "git" && fields[i+1] == "checkout" && fields[i+2] == "--" && fields[i+3] == "." {
			return "git checkout -- . discards all uncommitted changes"
		}
	}
	// Look for "git restore ." pattern
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] == "git" && fields[i+1] == "restore" && fields[i+2] == "." {
			return "git restore . discards all uncommitted changes"
		}
	}
	return ""
}

// matchPushToProtectedBranch blocks "git push origin main" and "git push origin master".
func matchPushToProtectedBranch(command string) string {
	if !strings.Contains(command, "git") || !strings.Contains(command, "push") {
		return ""
	}
	fields := strings.Fields(command)
	hasPush := false
	for i, f := range fields {
		if f == "push" && i > 0 && fields[i-1] == "git" {
			hasPush = true
			continue
		}
		if !hasPush {
			continue
		}
		// After "git push", look for "origin main" or "origin master"
		if f == "origin" && i+1 < len(fields) {
			next := fields[i+1]
			if next == "main" || next == "master" {
				return "Agents must use sol resolve → forge, not direct push to " + next
			}
		}
	}
	return ""
}

// matchManualBranching blocks "git checkout -b" and "git switch -c".
func matchManualBranching(command string) string {
	if !strings.Contains(command, "git") {
		return ""
	}
	fields := strings.Fields(command)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] == "git" && fields[i+1] == "checkout" && fields[i+2] == "-b" {
			return "Outposts already have their branch assigned — don't create manual branches"
		}
		if fields[i] == "git" && fields[i+1] == "switch" && fields[i+2] == "-c" {
			return "Outposts already have their branch assigned — don't create manual branches"
		}
	}
	return ""
}

// printGuardBlock prints a block banner to stderr.
func printGuardBlock(title, reason, originalCommand string) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  BLOCKED: %s\n", title)
	fmt.Fprintf(os.Stderr, "  Command: %s\n", originalCommand)
	fmt.Fprintf(os.Stderr, "  Reason:  %s\n", reason)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  If this is intentional, ask the user to run it manually.")
	fmt.Fprintln(os.Stderr, "")
}
