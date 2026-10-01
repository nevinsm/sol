package forge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

const (
	grepKey   = "git log deadbeef00000001..origin/main --oneline --grep sol-aaa11111"
	mtKey     = "git merge-tree --write-tree origin/main origin/outpost/Toast/sol-aaa11111"
	targetKey = "git rev-parse origin/main^{tree}"
)

func untaggedFixture(t *testing.T) (*patrolState, *mockWorldStore, *mockCmdRunner, *store.MergeRequest) {
	t.Helper()
	state, worldStore, _ := setupOrchestratorTest(t)
	t.Cleanup(func() { state.fl.Close() })
	mr := &store.MergeRequest{ID: "mr-001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "T", Status: store.WritDone}
	state.preMergeRef = "deadbeef00000001"
	state.verifyRetryDelay = 1
	cmd := state.cmd.(*mockCmdRunner)
	cmd.SetResult("git fetch origin", nil, nil)
	cmd.SetResult(grepKey, nil, nil)
	return state, worldStore, cmd, mr
}

func countCalls(cmd *mockCmdRunner, sub string) int {
	n := 0
	for _, c := range cmd.getCalls() {
		if strings.Contains(c.Name+" "+strings.Join(c.Args, " "), sub) {
			n++
		}
	}
	return n
}

func TestActOnResultContainmentLandsUntagged(t *testing.T) {
	state, worldStore, cmd, mr := untaggedFixture(t)
	state.eventLog = events.NewLogger(os.Getenv("SOL_HOME"))
	cmd.SetResult(mtKey, []byte("treeoid1\n"), nil)
	cmd.SetResult(targetKey, []byte("treeoid1\n"), nil)
	cmd.SetResult("git log deadbeef00000001..origin/main --format=%h %s", []byte("a27693d fix import order\n"), nil)

	state.actOnResult(context.Background(), mr, &ForgeResult{Result: "merged", Summary: "ok"}, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	md := worldStore.metadata["sol-aaa11111"]["merged-untagged"]
	worldStore.mu.Unlock()
	if phase != store.MRMerged {
		t.Fatalf("phase = %q, want merged", phase)
	}
	if s, _ := md.(string); !strings.Contains(s, "a27693d fix import order") {
		t.Errorf("merged-untagged metadata = %v", md)
	}
	evts, err := events.NewReader(os.Getenv("SOL_HOME"), false).Read(events.ReadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evts {
		if e.Type == events.EventMergeUntagged && e.Visibility == "both" {
			found = true
		}
	}
	if !found {
		t.Errorf("merge_untagged event not emitted: %+v", evts)
	}
}

func TestVerifyPushContainmentTreesDiffer(t *testing.T) {
	state, _, cmd, mr := untaggedFixture(t)
	cmd.SetResult(mtKey, []byte("treeoid2\n"), nil)
	cmd.SetResult(targetKey, []byte("treeoid1\n"), nil)
	vr := state.verifyPush(context.Background(), mr)
	if vr.landed {
		t.Fatalf("landed = true, want false")
	}
}

func TestActOnResultContainmentTreesDifferMarksFailed(t *testing.T) {
	state, worldStore, cmd, mr := untaggedFixture(t)
	cmd.SetResult(mtKey, []byte("treeoid2\n"), nil)
	cmd.SetResult(targetKey, []byte("treeoid1\n"), nil)
	state.actOnResult(context.Background(), mr, &ForgeResult{Result: "merged"}, 1)
	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if p := worldStore.phaseUpdates["mr-001"]; p == store.MRMerged {
		t.Errorf("phase = merged, want failed")
	}
	if len(worldStore.metadata) != 0 {
		t.Errorf("unexpected metadata: %v", worldStore.metadata)
	}
}

func TestVerifyPushContainmentConflict(t *testing.T) {
	state, _, cmd, mr := untaggedFixture(t)
	exit1 := exec.Command("sh", "-c", "exit 1").Run()
	cmd.SetResult(mtKey, []byte("CONFLICT"), exit1)
	vr := state.verifyPush(context.Background(), mr)
	if vr.landed {
		t.Fatalf("landed = true on conflict")
	}
}

func TestVerifyPushContainmentOtherErrorNotLanded(t *testing.T) {
	state, _, cmd, mr := untaggedFixture(t)
	exit129 := exec.Command("sh", "-c", "exit 129").Run()
	cmd.SetResult(mtKey, []byte("usage"), exit129)
	vr := state.verifyPush(context.Background(), mr)
	if vr.landed || vr.err == nil {
		t.Fatalf("want not landed with error, got %+v", vr)
	}
}

func TestVerifyPushTagHitSkipsContainment(t *testing.T) {
	state, _, cmd, mr := untaggedFixture(t)
	cmd.SetResult(grepKey, []byte("abc1234 Fix (sol-aaa11111)"), nil)
	vr := state.verifyPush(context.Background(), mr)
	if !vr.landed || vr.via != "source" {
		t.Fatalf("got %+v", vr)
	}
	if n := countCalls(cmd, "merge-tree"); n != 0 {
		t.Errorf("merge-tree ran %d times on tag hit", n)
	}
}

// untaggedRepo: main has a squash of the branch's work under a wrong subject.
func untaggedRepo(t *testing.T) (work, landed, extra string) {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	run(t, "git", "init", "--bare", "-b", "main", bare)
	work = filepath.Join(dir, "work")
	run(t, "git", "clone", bare, work)
	g := func(args ...string) string {
		return run(t, "git", append([]string{"-C", work}, args...)...)
	}
	g("config", "user.email", "t@t.com")
	g("config", "user.name", "T")
	g("checkout", "-b", "main")
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("base\n"), 0o644)
	g("add", ".")
	g("commit", "-m", "base")
	g("push", "origin", "main")

	landed = "outpost/Toast/sol-landed0001"
	g("checkout", "-b", landed)
	os.WriteFile(filepath.Join(work, "b.txt"), []byte("work\n"), 0o644)
	g("add", ".")
	g("commit", "-m", "work")
	g("push", "origin", landed)

	extra = "outpost/Toast/sol-extra00001"
	g("checkout", "-b", extra)
	os.WriteFile(filepath.Join(work, "c.txt"), []byte("more\n"), 0o644)
	g("add", ".")
	g("commit", "-m", "more")
	g("push", "origin", extra)

	// Squash `landed` into main under a wrong subject.
	g("checkout", "main")
	g("merge", "--squash", landed)
	g("commit", "-m", "fix import order")
	g("push", "origin", "main")
	g("fetch", "origin")
	return work, landed, extra
}

func TestTreeContainedInTargetRealRepo(t *testing.T) {
	work, landed, extra := untaggedRepo(t)
	ctx := context.Background()
	ok, err := treeContainedInTarget(ctx, &realCmdRunner{}, work, "origin/main", "origin/"+landed)
	if err != nil || !ok {
		t.Errorf("squash-landed branch: ok=%v err=%v, want contained", ok, err)
	}
	ok, err = treeContainedInTarget(ctx, &realCmdRunner{}, work, "origin/main", "origin/"+extra)
	if err != nil || ok {
		t.Errorf("branch with extra commit: ok=%v err=%v, want not contained", ok, err)
	}
}

func TestDeleteBranchContainmentNoEscalation(t *testing.T) {
	work, landed, extra := untaggedRepo(t)
	sphere := newMockSphereStore()
	cfg := DefaultConfig()
	cfg.TargetBranch = "main"
	r := &Forge{
		world: "ember", agentID: "ember/forge", sourceRepo: work, worktree: work,
		worldStore: newMockWorldStore(), sphereStore: sphere, logger: testLogger(),
		cfg: cfg, cmd: &realCmdRunner{},
	}

	r.deleteBranchGated("mr-1", landed, "sol-landed0001", "mr:mr-1", true)
	run(t, "git", "-C", work, "fetch", "origin", "--prune")
	if exec.Command("git", "-C", work, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+landed).Run() == nil {
		t.Errorf("contained branch was not deleted")
	}
	if len(sphere.escalations) != 0 {
		t.Errorf("unexpected escalations: %+v", sphere.escalations)
	}

	// Branch with unmerged work is kept (and escalated).
	r.deleteBranchGated("mr-2", extra, "sol-extra00001", "mr:mr-2", true)
	run(t, "git", "-C", work, "fetch", "origin", "--prune")
	if exec.Command("git", "-C", work, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+extra).Run() != nil {
		t.Errorf("unmerged branch was deleted")
	}
	_ = store.MRMerged
}
