package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestForgeSyncCLI(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnvWithRepo(t)

	// Create a bare repo and working clone.
	bareRepo, workingClone := createSourceRepo(t, gtHome)

	// Initialize world with the bare repo as source.
	setupWorld(t, gtHome, "synctest", bareRepo)

	// Start forge to create the worktree.
	_, err := runGT(t, gtHome, "forge", "start", "--world=synctest")
	if err != nil {
		t.Fatalf("forge start failed: %v", err)
	}
	// Capture bin path now (gtBin may call t.Fatal, which is undefined in cleanup).
	forgeBin := gtBin(t)
	t.Cleanup(func() {
		// Non-fatal stop: calling t.Fatal from t.Cleanup panics in Go 1.21+.
		cmd := exec.Command(forgeBin, "forge", "stop", "--world=synctest")
		cmd.Dir = os.TempDir()
		cmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup: forge stop synctest: %v: %s", err, strings.TrimSpace(string(out)))
		}
	})

	// Push a new commit from the working clone.
	writeTestFile(t, filepath.Join(workingClone, "sync-test.txt"), "synced content")
	runGit(t, workingClone, "add", ".")
	runGit(t, workingClone, "commit", "-m", "add sync-test file")
	runGit(t, workingClone, "push", "origin", "main")

	// Run forge sync.
	out, err := runGT(t, gtHome, "forge", "sync", "--world=synctest")
	if err != nil {
		t.Fatalf("forge sync failed: %v: %s", err, out)
	}

	// Verify the forge worktree has the new file.
	forgeWT := filepath.Join(gtHome, "synctest", "forge", "worktree")
	data, err := os.ReadFile(filepath.Join(forgeWT, "sync-test.txt"))
	if err != nil {
		t.Fatalf("file not found in forge worktree after sync: %v", err)
	}
	if string(data) != "synced content" {
		t.Errorf("expected 'synced content', got %q", string(data))
	}
}

func TestWorldSyncAllCLI(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnvWithRepo(t)

	// Create a bare repo and working clone.
	bareRepo, workingClone := createSourceRepo(t, gtHome)

	// Initialize world with the bare repo as source.
	setupWorld(t, gtHome, "syncall", bareRepo)

	// Start forge to create the worktree.
	_, err := runGT(t, gtHome, "forge", "start", "--world=syncall")
	if err != nil {
		t.Fatalf("forge start failed: %v", err)
	}
	// Capture bin path now (gtBin may call t.Fatal, which is undefined in cleanup).
	forgeBin := gtBin(t)
	t.Cleanup(func() {
		// Non-fatal stop: calling t.Fatal from t.Cleanup panics in Go 1.21+.
		cmd := exec.Command(forgeBin, "forge", "stop", "--world=syncall")
		cmd.Dir = os.TempDir()
		cmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup: forge stop syncall: %v: %s", err, strings.TrimSpace(string(out)))
		}
	})

	// Push a new commit from the working clone.
	writeTestFile(t, filepath.Join(workingClone, "all-sync.txt"), "all synced")
	runGit(t, workingClone, "add", ".")
	runGit(t, workingClone, "commit", "-m", "add all-sync file")
	runGit(t, workingClone, "push", "origin", "main")

	// Run world sync --all.
	out, err := runGT(t, gtHome, "world", "sync", "--world=syncall", "--all")
	if err != nil {
		t.Fatalf("world sync --all failed: %v: %s", err, out)
	}

	// Verify managed repo has the new file.
	repoPath := filepath.Join(gtHome, "syncall", "repo")
	data, err := os.ReadFile(filepath.Join(repoPath, "all-sync.txt"))
	if err != nil {
		t.Fatalf("file not found in managed repo after sync: %v", err)
	}
	if string(data) != "all synced" {
		t.Errorf("expected 'all synced', got %q", string(data))
	}

	// Verify forge worktree also has the new file.
	forgeWT := filepath.Join(gtHome, "syncall", "forge", "worktree")
	data, err = os.ReadFile(filepath.Join(forgeWT, "all-sync.txt"))
	if err != nil {
		t.Fatalf("file not found in forge worktree after sync --all: %v", err)
	}
	if string(data) != "all synced" {
		t.Errorf("expected 'all synced', got %q", string(data))
	}
}
