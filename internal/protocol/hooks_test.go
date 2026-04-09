package protocol

import (
	"testing"
)

func TestRoleGuardsOutpost(t *testing.T) {
	guards := RoleGuards("outpost")
	// Outpost: 6 common (force push, 4 rm variants, git worktree remove)
	// + 4 dangerous-command (git reset, clean, checkout --, restore)
	// + 3 workflow-bypass (branch create, push main, gh pr) = 13
	if len(guards) != 13 {
		t.Fatalf("expected 13 guards for outpost, got %d", len(guards))
	}
}

func TestRoleGuardsForge(t *testing.T) {
	guards := RoleGuards("forge")
	// Forge: 6 dangerous-command only
	// (force push, 4 rm variants, git worktree remove)
	if len(guards) != 6 {
		t.Fatalf("expected 6 guards for forge, got %d", len(guards))
	}

	// Regression: forge must NOT carry the git reset/clean/restore/checkout
	// exemptions — merge recovery legitimately needs those commands.
	forbidden := []string{
		"Bash(git reset --hard*)",
		"Bash(git clean -f*)",
		"Bash(git checkout -- *)",
		"Bash(git restore *)",
		"Bash(git checkout -b*)|Bash(git switch -c*)",
		"Bash(git push origin main*)|Bash(git push origin master*)",
		"Bash(gh pr create*)",
	}
	for _, pat := range forbidden {
		for _, g := range guards {
			if g.Pattern == pat {
				t.Errorf("forge guards must not contain %q (merge recovery needs it)", pat)
			}
		}
	}
}

func TestRoleGuardsBlocksRelativeRm(t *testing.T) {
	required := []string{
		"Bash(rm -rf*)",
		"Bash(rm -fr*)",
		"Bash(rm -r -f*)",
		"Bash(rm -f -r*)",
	}
	for _, role := range []string{"outpost", "forge"} {
		guards := RoleGuards(role)
		for _, pat := range required {
			found := false
			for _, g := range guards {
				if g.Pattern == pat && g.Command == "sol guard dangerous-command" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("role %q missing required rm guard %q", role, pat)
			}
		}
	}
}

func TestRoleGuardsBlocksGitWorktreeRemove(t *testing.T) {
	pat := "Bash(git worktree remove*)"
	for _, role := range []string{"outpost", "forge"} {
		guards := RoleGuards(role)
		found := false
		for _, g := range guards {
			if g.Pattern == pat && g.Command == "sol guard dangerous-command" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("role %q missing guard %q", role, pat)
		}
	}
}
