package forge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const hookTitle = `Fix "quotes" and ` + "`tick`" + ` with $VAR`

func TestMergeHooksCommitMsgRewrites(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	dir, err := WriteMergeHooks("w", hookTitle, "sol-abc123")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644)
	gitRun(t, repo, "add", "f")
	gitRun(t, repo, "-c", "core.hooksPath="+dir, "commit", "-q", "-m", "wrong message")
	got := gitRun(t, repo, "log", "-1", "--format=%B")
	want := hookTitle + " (sol-abc123)"
	if got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestMergeHooksPrePush(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	dir, err := WriteMergeHooks("w", "T", "sol-abc123")
	if err != nil {
		t.Fatal(err)
	}
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare")
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "remote", "add", "origin", bare)
	os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644)
	gitRun(t, repo, "add", "f")
	gitRun(t, repo, "commit", "-q", "-m", "fix import order")

	push := exec.Command("git", "-c", "core.hooksPath="+dir, "push", "origin", "HEAD:refs/heads/main")
	push.Dir = repo
	out, err := push.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "(sol-abc123)") {
		t.Fatalf("expected push refusal naming tag, err=%v out=%s", err, out)
	}

	gitRun(t, repo, "commit", "-q", "--amend", "-m", "T (sol-abc123)")
	gitRun(t, repo, "-c", "core.hooksPath="+dir, "push", "origin", "HEAD:refs/heads/main")
}

func TestWriteMergeHooksOverwrites(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	if _, err := WriteMergeHooks("w", "First", "sol-1"); err != nil {
		t.Fatal(err)
	}
	dir, err := WriteMergeHooks("w", "Second", "sol-2")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "message.txt"))
	if string(b) != "Second (sol-2)\n" {
		t.Errorf("stale message.txt: %q", b)
	}
	pp, _ := os.ReadFile(filepath.Join(dir, "pre-push"))
	if strings.Contains(string(pp), "sol-1") {
		t.Error("stale pre-push")
	}
	env := MergeHooksEnv(dir)
	if env["GIT_CONFIG_VALUE_0"] != dir || env["GIT_CONFIG_COUNT"] != "1" {
		t.Errorf("bad env: %v", env)
	}
}
