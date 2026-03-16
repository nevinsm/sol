package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// --- extractCommand ---

func TestGuardExtractCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bash command", `{"tool_input":{"command":"git push --force"}}`, "git push --force"},
		{"empty input", "", ""},
		{"invalid json", "not json", ""},
		{"no command field", `{"tool_input":{}}`, ""},
		{"nested tool_input", `{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`, "rm -rf /"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := guardExtractCommand([]byte(tt.input))
			if got != tt.want {
				t.Errorf("guardExtractCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- dangerous-command matching ---

func TestMatchDangerousGitPush(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"force push", "git push --force", true},
		{"force push short", "git push -f", true},
		{"force push with remote", "git push origin main --force", true},
		{"force-with-lease allowed", "git push --force-with-lease", false},
		{"force-if-includes allowed", "git push --force-if-includes", false},
		{"normal push", "git push origin main", false},
		{"unrelated", "echo hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := matchDangerousGitPush(tt.command)
			if (reason != "") != tt.blocked {
				t.Errorf("matchDangerousGitPush(%q) blocked=%v, want blocked=%v (reason=%q)",
					tt.command, reason != "", tt.blocked, reason)
			}
		})
	}
}

func TestMatchDangerousRmRf(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"rm -rf /", "rm -rf /", true},
		{"rm -rf /*", "rm -rf /*", true},
		{"rm -rf ./build/ allowed", "rm -rf ./build/", false},
		{"rm -rf tmp allowed", "rm -rf tmp", false},
		{"unrelated", "echo hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := matchDangerousRmRf(tt.command)
			if (reason != "") != tt.blocked {
				t.Errorf("matchDangerousRmRf(%q) blocked=%v, want blocked=%v",
					tt.command, reason != "", tt.blocked)
			}
		})
	}
}

func TestMatchDangerousCheckoutRestore(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"checkout -- .", "git checkout -- .", true},
		{"restore .", "git restore .", true},
		{"checkout specific file allowed", "git checkout -- file.go", false},
		{"restore specific file allowed", "git restore file.go", false},
		{"unrelated", "git status", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := matchDangerousCheckoutRestore(tt.command)
			if (reason != "") != tt.blocked {
				t.Errorf("matchDangerousCheckoutRestore(%q) blocked=%v, want blocked=%v",
					tt.command, reason != "", tt.blocked)
			}
		})
	}
}

func TestFragmentPatterns(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"git reset --hard", "git reset --hard", true},
		{"git reset --hard HEAD~1", "git reset --hard HEAD~1", true},
		{"git reset --soft allowed", "git reset --soft HEAD~1", false},
		{"git clean -f", "git clean -f", true},
		{"git clean -fd", "git clean -fd", true},
		{"git clean -n allowed", "git clean -n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lower := tt.command
			blocked := false
			for _, pattern := range fragmentPatterns {
				if matchAllFragments(lower, pattern.contains) {
					blocked = true
					break
				}
			}
			if blocked != tt.blocked {
				t.Errorf("fragmentPatterns for %q: blocked=%v, want blocked=%v",
					tt.command, blocked, tt.blocked)
			}
		})
	}
}

// --- workflow-bypass matching ---

func TestMatchPushToProtectedBranch(t *testing.T) {
	// Without SOL_WORLD set, the function fails open — all pushes are allowed.
	t.Setenv("SOL_WORLD", "")
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"push origin main - fail open without world", "git push origin main", false},
		{"push origin master - fail open without world", "git push origin master", false},
		{"push origin feature-branch allowed", "git push origin feature-branch", false},
		{"push without remote allowed", "git push", false},
		{"unrelated", "echo hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := matchPushToProtectedBranch(tt.command)
			if (reason != "") != tt.blocked {
				t.Errorf("matchPushToProtectedBranch(%q) blocked=%v, want blocked=%v (reason=%q)",
					tt.command, reason != "", tt.blocked, reason)
			}
		})
	}
}

func TestMatchPushToProtectedBranchWithWorldConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	t.Setenv("SOL_WORLD", "testworld")

	// Create world config with branch="main" and protected_branches.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldToml := "[world]\nbranch = \"main\"\nprotected_branches = [\"release/*\", \"staging\"]\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(worldToml), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"push to world branch", "git push origin main", true},
		{"push to protected glob release/1.0", "git push origin release/1.0", true},
		{"push to protected glob release/2.0-rc", "git push origin release/2.0-rc", true},
		{"push to protected exact staging", "git push origin staging", true},
		{"push to feature branch allowed", "git push origin feature-branch", false},
		{"push to outpost branch allowed", "git push origin outpost/Nova/sol-abc", false},
		{"push without remote allowed", "git push", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := matchPushToProtectedBranch(tt.command)
			if (reason != "") != tt.blocked {
				t.Errorf("matchPushToProtectedBranch(%q) blocked=%v, want blocked=%v (reason=%q)",
					tt.command, reason != "", tt.blocked, reason)
			}
		})
	}
}

func TestMatchManualBranching(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"checkout -b", "git checkout -b feature", true},
		{"switch -c", "git switch -c feature", true},
		{"checkout existing branch allowed", "git checkout main", false},
		{"switch existing branch allowed", "git switch main", false},
		{"unrelated", "git status", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := matchManualBranching(tt.command)
			if (reason != "") != tt.blocked {
				t.Errorf("matchManualBranching(%q) blocked=%v, want blocked=%v",
					tt.command, reason != "", tt.blocked)
			}
		})
	}
}

func TestWorkflowBypassPatterns(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"gh pr create", "gh pr create", true},
		{"gh pr create with args", "gh pr create --title foo", true},
		{"gh pr view allowed", "gh pr view", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lower := tt.command
			blocked := false
			for _, pattern := range workflowBypassPatterns {
				if matchAllFragments(lower, pattern.contains) {
					blocked = true
					break
				}
			}
			if blocked != tt.blocked {
				t.Errorf("workflowBypassPatterns for %q: blocked=%v, want blocked=%v",
					tt.command, blocked, tt.blocked)
			}
		})
	}
}
