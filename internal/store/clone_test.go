package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestCloneWorldDataPreservesWritColumns(t *testing.T) {
	// This test cannot be parallelized: CloneWorldData reads SOL_HOME to locate
	// databases, and t.Setenv cannot be used in parallel tests.
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create source world and populate writs with non-default values.
	src, err := OpenWorld("source")
	if err != nil {
		t.Fatal(err)
	}

	id1, err := src.CreateWritWithOpts(CreateWritOpts{
		Title:       "task with metadata",
		Description: "has kind, metadata, and close_reason",
		CreatedBy:   "test",
		Kind:        "research",
		Metadata:    map[string]any{"env": "staging", "count": float64(42)},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close the writ with a reason so close_reason is populated.
	if _, err := src.CloseWrit(id1, "completed-successfully"); err != nil {
		t.Fatal(err)
	}

	// Create a second writ with defaults (kind=code, no metadata, no close_reason)
	// to ensure the clone also handles default values correctly.
	id2, err := src.CreateWritWithOpts(CreateWritOpts{
		Title:       "default writ",
		Description: "uses defaults",
		CreatedBy:   "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	src.Close()

	// Create target world.
	tgt, err := OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	tgt.Close()

	// Clone source → target.
	if err := CloneWorldData("source", "target", false); err != nil {
		t.Fatalf("CloneWorldData failed: %v", err)
	}

	// Reopen target and verify cloned data.
	tgt, err = OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	defer tgt.Close()

	// Verify writ with non-default kind, metadata, and close_reason.
	w1, err := tgt.GetWrit(id1)
	if err != nil {
		t.Fatalf("GetWrit(%q) failed: %v", id1, err)
	}
	if w1.Kind != "research" {
		t.Errorf("kind = %q, want %q", w1.Kind, "research")
	}
	if w1.CloseReason != "completed-successfully" {
		t.Errorf("close_reason = %q, want %q", w1.CloseReason, "completed-successfully")
	}
	if w1.Metadata == nil {
		t.Fatal("metadata is nil, want non-nil")
	}
	if w1.Metadata["env"] != "staging" {
		t.Errorf("metadata[env] = %v, want %q", w1.Metadata["env"], "staging")
	}
	if w1.Metadata["count"] != float64(42) {
		t.Errorf("metadata[count] = %v, want %v", w1.Metadata["count"], float64(42))
	}
	if w1.ClosedAt == nil {
		t.Error("closed_at is nil, want non-nil for closed writ")
	}

	// Verify writ with default values.
	w2, err := tgt.GetWrit(id2)
	if err != nil {
		t.Fatalf("GetWrit(%q) failed: %v", id2, err)
	}
	if w2.Kind != "code" {
		t.Errorf("kind = %q, want %q", w2.Kind, "code")
	}
	if w2.CloseReason != "" {
		t.Errorf("close_reason = %q, want empty", w2.CloseReason)
	}
	if w2.Metadata != nil {
		t.Errorf("metadata = %v, want nil", w2.Metadata)
	}
}

func TestCloneWorldDataPreservesTokenUsageAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create source world with token usage that has a non-empty account.
	src, err := OpenWorld("source")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	histID, err := src.WriteHistory("Toast", "sol-item01", "cast", "work", now, nil)
	if err != nil {
		t.Fatal(err)
	}

	cost := 1.25
	_, err = src.WriteTokenUsage(histID, "claude-sonnet-4-6", 1000, 500, 200, 100, 0, &cost, nil, "claude-code", "personal")
	if err != nil {
		t.Fatal(err)
	}
	src.Close()

	// Create target world.
	tgt, err := OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	tgt.Close()

	// Clone with history.
	if err := CloneWorldData("source", "target", true); err != nil {
		t.Fatalf("CloneWorldData failed: %v", err)
	}

	// Reopen target and verify account survived.
	tgt, err = OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	defer tgt.Close()

	spend, err := tgt.DailySpendByAccount("personal")
	if err != nil {
		t.Fatalf("DailySpendByAccount failed: %v", err)
	}
	if spend != 1.25 {
		t.Errorf("cloned account spend = %f, want 1.25", spend)
	}
}

func TestCloneWorldDataPreservesReasoningTokens(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create source world with token usage that has non-zero reasoning_tokens.
	src, err := OpenWorld("source")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	histID, err := src.WriteHistory("Toast", "sol-item01", "cast", "work", now, nil)
	if err != nil {
		t.Fatal(err)
	}

	cost := 2.50
	_, err = src.WriteTokenUsage(histID, "claude-sonnet-4", 1000, 500, 200, 100, 7500, &cost, nil, "claude-code", "")
	if err != nil {
		t.Fatal(err)
	}
	src.Close()

	// Create target world.
	tgt, err := OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	tgt.Close()

	// Clone with history.
	if err := CloneWorldData("source", "target", true); err != nil {
		t.Fatalf("CloneWorldData failed: %v", err)
	}

	// Reopen target and verify reasoning_tokens survived.
	tgt, err = OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	defer tgt.Close()

	ts, err := tgt.TokensForHistory(histID)
	if err != nil {
		t.Fatalf("TokensForHistory failed: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil token summary")
	}
	if ts.ReasoningTokens != 7500 {
		t.Errorf("cloned reasoning_tokens = %d, want 7500", ts.ReasoningTokens)
	}
}

// TestCloneColumnListsMatchSchema is a regression test that catches schema
// drift between the database migrations and clone.go's column lists.
//
// CloneWorldData has historically silently dropped data when migrations added
// new columns to cloned tables but the corresponding cloneXxxColumns slice in
// clone.go was not updated (see commits 443d3ea, 361f3bc, 3a526ef, and the
// resolution_count fix that introduced this test). This test enumerates the
// actual schema columns via PRAGMA table_info on a freshly migrated database
// and asserts they match the column lists used by CloneWorldData.
//
// When this test fails after a migration: add the missing column(s) to the
// matching cloneXxxColumns slice in clone.go.
func TestCloneColumnListsMatchSchema(t *testing.T) {
	// Cannot be parallelized: OpenWorld reads SOL_HOME via t.Setenv.
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// OpenWorld runs migrateWorld, leaving the database at CurrentWorldSchema.
	s, err := OpenWorld("schema-check")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	cases := []struct {
		table string
		clone []string
	}{
		{"writs", cloneWritsColumns},
		{"labels", cloneLabelsColumns},
		{"dependencies", cloneDependenciesColumns},
		{"merge_requests", cloneMergeRequestsColumns},
		{"agent_history", cloneAgentHistoryColumns},
		{"token_usage", cloneTokenUsageColumns},
	}

	for _, tc := range cases {
		schemaCols, err := tableColumns(s.DB(), tc.table)
		if err != nil {
			t.Fatalf("table_info(%s): %v", tc.table, err)
		}
		sort.Strings(schemaCols)

		cloneSorted := append([]string(nil), tc.clone...)
		sort.Strings(cloneSorted)

		if !reflect.DeepEqual(schemaCols, cloneSorted) {
			missing := diffStrings(schemaCols, cloneSorted)
			extra := diffStrings(cloneSorted, schemaCols)
			t.Errorf("table %q clone columns out of sync with schema:\n"+
				"  schema columns: %v\n"+
				"  clone columns:  %v\n"+
				"  missing from clone.go: %v\n"+
				"  extra in clone.go:     %v\n"+
				"Add the missing columns to clone%sColumns in clone.go.",
				tc.table, schemaCols, cloneSorted, missing, extra, snakeToCamel(tc.table))
		}
	}
}

// tableColumns returns the column names of the given table via PRAGMA
// table_info, which reflects the actual schema after migrations have run.
func tableColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

// diffStrings returns the elements of a not present in b. Both inputs are
// expected to be sorted.
func diffStrings(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[s] = struct{}{}
	}
	var out []string
	for _, s := range a {
		if _, ok := set[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}

// TestCloneWorldDataConcurrent verifies that CloneWorldData is safe to run
// concurrently against distinct target worlds. Prior to pinning ATTACH and
// the subsequent statements to a single *sql.Conn, concurrent callers could
// interleave on the target DB's connection pool so that BEGIN (or the data
// copies) landed on a connection where the source database had never been
// attached. This test stresses that scenario.
func TestCloneWorldDataConcurrent(t *testing.T) {
	// Cannot be parallel — uses t.Setenv.
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a single shared source world with a writ.
	src, err := OpenWorld("source")
	if err != nil {
		t.Fatal(err)
	}
	wantID, err := src.CreateWritWithOpts(CreateWritOpts{
		Title:     "shared source writ",
		CreatedBy: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	src.Close()

	const N = 8
	// Pre-create all target worlds so CloneWorldData just has to copy into them.
	for i := 0; i < N; i++ {
		name := fmt.Sprintf("target%d", i)
		tgt, err := OpenWorld(name)
		if err != nil {
			t.Fatal(err)
		}
		tgt.Close()
	}

	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("target%d", idx)
			if err := CloneWorldData("source", name, false); err != nil {
				errs <- fmt.Errorf("clone %d: %w", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("%v", err)
	}

	// Verify every target received the cloned writ.
	for i := 0; i < N; i++ {
		name := fmt.Sprintf("target%d", i)
		tgt, err := OpenWorld(name)
		if err != nil {
			t.Fatalf("reopen target%d: %v", i, err)
		}
		w, err := tgt.GetWrit(wantID)
		if err != nil {
			tgt.Close()
			t.Errorf("target%d missing cloned writ: %v", i, err)
			continue
		}
		if w.Title != "shared source writ" {
			t.Errorf("target%d title = %q, want %q", i, w.Title, "shared source writ")
		}
		tgt.Close()
	}
}

// snakeToCamel converts a snake_case table name to UpperCamelCase for use in
// error messages that point at the matching cloneXxxColumns variable.
func snakeToCamel(s string) string {
	out := make([]byte, 0, len(s))
	upper := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			upper = true
			continue
		}
		if upper && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		upper = false
		out = append(out, c)
	}
	return string(out)
}
