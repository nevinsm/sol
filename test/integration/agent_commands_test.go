package integration

// agent_commands_test.go — Integration tests for agent-facing CLI commands
// that previously had no CLI-level coverage:
//   sol mr       (merge request plumbing)
//   sol nudge    (nudge queue operations)
//   sol guard    (block forbidden operations)
//   sol skill    (skill management)
//   sol docs     (documentation tools)
//
// All tests use setupTestEnv() from helpers_test.go for isolation.
// None spawn real claude processes or touch the real tmux server.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGTWithStdin runs the sol binary like runGT but pipes stdinData to stdin.
// Used by guard tests which read the Claude Code hook protocol from stdin.
func runGTWithStdin(t *testing.T, gtHome, stdinData string, args ...string) (string, error) {
	t.Helper()
	bin := gtBin(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
	cmd.Stdin = strings.NewReader(stdinData)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// exitCode extracts the exit code from an exec error. Returns 0 if err is nil.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// ---------- sol mr ----------

func TestCLIMrHelp(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "mr", "--help")
	if err != nil {
		t.Fatalf("sol mr --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Merge request") {
		t.Errorf("mr help missing description, got: %s", out)
	}
}

func TestCLIMrCreateMissingBranch(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "mr", "create", "--world=ember", "--writ=sol-0000000000000000")
	if err == nil {
		t.Fatalf("expected error for missing --branch, got: %s", out)
	}
	if !strings.Contains(out, `required flag(s) "branch" not set`) {
		t.Errorf("expected 'required flag(s) \"branch\" not set' error, got: %s", out)
	}
}

func TestCLIMrCreateMissingWrit(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "mr", "create", "--world=ember", "--branch=test-branch")
	if err == nil {
		t.Fatalf("expected error for missing --writ, got: %s", out)
	}
	if !strings.Contains(out, `required flag(s) "writ" not set`) {
		t.Errorf("expected 'required flag(s) \"writ\" not set' error, got: %s", out)
	}
}

func TestCLIMrCreateHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Create a writ first.
	writOut, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=mr test writ")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// Create the merge request.
	out, err := runGT(t, gtHome, "mr", "create", "--world=ember", "--branch=outpost/test/"+writID, "--writ="+writID)
	if err != nil {
		t.Fatalf("mr create failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Created:") {
		t.Errorf("expected 'Created:' in output, got: %s", out)
	}
	if !strings.Contains(out, writID) {
		t.Errorf("expected writ ID %s in output, got: %s", writID, out)
	}
	if !strings.Contains(out, "Branch:") {
		t.Errorf("expected 'Branch:' in output, got: %s", out)
	}
}

func TestCLIMrCreateJSON(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	writOut, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=mr json test")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	out, err := runGT(t, gtHome, "mr", "create", "--world=ember", "--branch=outpost/json/"+writID, "--writ="+writID, "--json")
	if err != nil {
		t.Fatalf("mr create --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("mr create --json output is not valid JSON: %s", out)
	}
}

func TestCLIMrCreateInvalidWrit(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "mr", "create", "--world=ember", "--branch=test-branch", "--writ=sol-doesnotexist0000")
	if err == nil {
		t.Fatalf("expected error for non-existent writ, got: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' error, got: %s", out)
	}
}

// ---------- sol nudge ----------

func TestCLINudgeHelp(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "nudge", "--help")
	if err != nil {
		t.Fatalf("sol nudge --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Nudge queue") {
		t.Errorf("nudge help missing description, got: %s", out)
	}
}

func TestCLINudgeCountEmpty(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "nudge", "count", "--world=ember", "--agent=Toast")
	if err != nil {
		t.Fatalf("nudge count failed: %v: %s", err, out)
	}
	if strings.TrimSpace(out) != "0" {
		t.Errorf("expected count 0, got: %q", out)
	}
}

func TestCLINudgeCountJSON(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "nudge", "count", "--world=ember", "--agent=Toast", "--json")
	if err != nil {
		t.Fatalf("nudge count --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("nudge count --json output is not valid JSON: %s", out)
	}
	if !strings.Contains(out, "pending_count") {
		t.Errorf("expected 'pending_count' in JSON, got: %s", out)
	}
}

func TestCLINudgeListEmpty(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "nudge", "list", "--world=ember", "--agent=Toast")
	if err != nil {
		t.Fatalf("nudge list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No pending messages") {
		t.Errorf("expected 'No pending messages', got: %s", out)
	}
}

func TestCLINudgeDrainEmpty(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Drain with no pending messages should succeed silently.
	out, err := runGT(t, gtHome, "nudge", "drain", "--world=ember", "--agent=Toast")
	if err != nil {
		t.Fatalf("nudge drain failed: %v: %s", err, out)
	}
}

func TestCLINudgeMissingAgent(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Clear SOL_AGENT so the agent can't be resolved from the environment.
	t.Setenv("SOL_AGENT", "")

	// Count without --agent (and no SOL_AGENT env) should error.
	out, err := runGT(t, gtHome, "nudge", "count", "--world=ember")
	if err == nil {
		t.Fatalf("expected error for missing --agent, got: %s", out)
	}
	if !strings.Contains(out, "--agent") {
		t.Errorf("expected error mentioning --agent, got: %s", out)
	}
}

// ---------- sol guard ----------

func TestCLIGuardHelp(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "guard", "--help")
	if err != nil {
		t.Fatalf("sol guard --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Block forbidden operations") {
		t.Errorf("guard help missing description, got: %s", out)
	}
}

func TestCLIGuardDangerousBlocksForcePush(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	input := `{"tool_input":{"command":"git push --force origin main"}}`
	out, err := runGTWithStdin(t, gtHome, input, "guard", "dangerous-command")
	if err == nil {
		t.Fatalf("expected guard to block force push, got success: %s", out)
	}
	if exitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d: %s", exitCode(err), out)
	}
	if !strings.Contains(out, "BLOCKED") {
		t.Errorf("expected BLOCKED in output, got: %s", out)
	}
}

func TestCLIGuardDangerousAllowsSafeCommands(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	// A normal git commit should not be blocked.
	input := `{"tool_input":{"command":"git commit -m 'test'"}}`
	out, err := runGTWithStdin(t, gtHome, input, "guard", "dangerous-command")
	if err != nil {
		t.Fatalf("expected safe command to pass, got error: %v: %s", err, out)
	}
}

func TestCLIGuardDangerousBlocksHardReset(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	input := `{"tool_input":{"command":"git reset --hard HEAD~3"}}`
	out, err := runGTWithStdin(t, gtHome, input, "guard", "dangerous-command")
	if err == nil {
		t.Fatalf("expected guard to block hard reset, got success: %s", out)
	}
	if exitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d: %s", exitCode(err), out)
	}
}

func TestCLIGuardDangerousAllowsForceWithLease(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	input := `{"tool_input":{"command":"git push --force-with-lease origin feature"}}`
	out, err := runGTWithStdin(t, gtHome, input, "guard", "dangerous-command")
	if err != nil {
		t.Fatalf("expected --force-with-lease to pass, got error: %v: %s", err, out)
	}
}

func TestCLIGuardDangerousEmptyStdin(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	// Empty stdin should fail open (exit 0).
	out, err := runGTWithStdin(t, gtHome, "", "guard", "dangerous-command")
	if err != nil {
		t.Fatalf("expected empty stdin to fail open, got error: %v: %s", err, out)
	}
}

func TestCLIGuardWorkflowBypassBlocksGhPrCreate(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	input := `{"tool_input":{"command":"gh pr create --title 'test' --body 'test'"}}`
	out, err := runGTWithStdin(t, gtHome, input, "guard", "workflow-bypass")
	if err == nil {
		t.Fatalf("expected guard to block gh pr create, got success: %s", out)
	}
	if exitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d: %s", exitCode(err), out)
	}
	if !strings.Contains(out, "BLOCKED") {
		t.Errorf("expected BLOCKED in output, got: %s", out)
	}
}

func TestCLIGuardWorkflowBypassBlocksManualBranching(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	input := `{"tool_input":{"command":"git checkout -b my-feature"}}`
	out, err := runGTWithStdin(t, gtHome, input, "guard", "workflow-bypass")
	if err == nil {
		t.Fatalf("expected guard to block manual branching, got success: %s", out)
	}
	if exitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d: %s", exitCode(err), out)
	}
}

// ---------- sol skill ----------

func TestCLISkillHelp(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "skill", "--help")
	if err != nil {
		t.Fatalf("sol skill --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Skill management") {
		t.Errorf("skill help missing description, got: %s", out)
	}
}

func TestCLISkillExportHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	outputDir := t.TempDir()

	out, err := runGT(t, gtHome, "skill", "export", "--output="+outputDir)
	if err != nil {
		t.Fatalf("skill export failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Exported sol-integration skill to") {
		t.Errorf("expected export success message, got: %s", out)
	}

	// Verify the exported directory exists.
	destDir := filepath.Join(outputDir, "sol-integration")
	info, err := os.Stat(destDir)
	if err != nil {
		t.Fatalf("exported directory not found: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory at %s", destDir)
	}

	// Verify at least one file was exported.
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("failed to read exported directory: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("exported directory is empty")
	}
}

func TestCLISkillExportOverwrites(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	outputDir := t.TempDir()

	// Export twice — second should overwrite without error.
	if _, err := runGT(t, gtHome, "skill", "export", "--output="+outputDir); err != nil {
		t.Fatalf("first export failed: %v", err)
	}
	out, err := runGT(t, gtHome, "skill", "export", "--output="+outputDir)
	if err != nil {
		t.Fatalf("second export (overwrite) failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Exported sol-integration skill to") {
		t.Errorf("expected export success message on overwrite, got: %s", out)
	}
}

// ---------- sol docs ----------

func TestCLIDocsHelp(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "docs", "--help")
	if err != nil {
		t.Fatalf("sol docs --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Documentation tools") {
		t.Errorf("docs help missing description, got: %s", out)
	}
}

func TestCLIDocsGenerateStdout(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "docs", "generate", "--stdout")
	if err != nil {
		t.Fatalf("docs generate --stdout failed: %v: %s", err, out)
	}
	if len(out) == 0 {
		t.Errorf("docs generate --stdout produced no output")
	}
	// The generated CLI docs should contain at least the root command.
	if !strings.Contains(out, "sol") {
		t.Errorf("expected 'sol' in generated docs, got: %s", out[:min(200, len(out))])
	}
}

func TestCLIDocsValidateHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	// Create a temp directory that looks like a repo root.
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "docs"), 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	bin := gtBin(t)

	// Generate docs directly to the temp directory (writes docs/cli.md).
	genCmd := exec.Command(bin, "docs", "generate")
	genCmd.Dir = tempDir
	genCmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("docs generate failed: %v: %s", err, string(out))
	}

	// Validate should pass since we just generated.
	valCmd := exec.Command(bin, "docs", "validate")
	valCmd.Dir = tempDir
	valCmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
	valOut, valErr := valCmd.CombinedOutput()
	if valErr != nil {
		t.Fatalf("docs validate failed: %v: %s", valErr, string(valOut))
	}
	if !strings.Contains(string(valOut), "up to date") {
		t.Errorf("expected 'up to date' message, got: %s", string(valOut))
	}
}

