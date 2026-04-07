package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
)

// Clone column lists — these enumerate every column copied by CloneWorldData
// for each table. They MUST stay in sync with the actual database schema
// defined in schema.go. The TestCloneColumnListsMatchSchema regression test
// fails if a migration adds a column without updating the corresponding list
// here. This bug class has recurred multiple times (see commits 443d3ea,
// 361f3bc, 3a526ef) — every new column on a cloned table needs an entry here.
var (
	cloneWritsColumns = []string{
		"id", "title", "description", "status", "priority", "assignee",
		"parent_id", "kind", "metadata", "close_reason", "created_by",
		"created_at", "updated_at", "closed_at",
	}

	cloneLabelsColumns = []string{"writ_id", "label"}

	cloneDependenciesColumns = []string{"from_id", "to_id"}

	cloneMergeRequestsColumns = []string{
		"id", "writ_id", "branch", "phase", "claimed_by", "claimed_at",
		"attempts", "priority", "created_at", "updated_at", "merged_at",
		"blocked_by", "resolution_count",
	}

	cloneAgentHistoryColumns = []string{
		"id", "agent_name", "writ_id", "action", "started_at", "ended_at", "summary",
	}

	cloneTokenUsageColumns = []string{
		"id", "history_id", "model", "input_tokens", "output_tokens",
		"cache_read_tokens", "cache_creation_tokens", "cost_usd",
		"duration_ms", "runtime", "account", "reasoning_tokens",
	}
)

// buildCloneInsert constructs an INSERT … SELECT statement that copies the
// given columns from src.<table> into main.<table>. Any column listed in
// overrides is replaced in the SELECT clause by its override expression
// (e.g. "NULL" or "0") to clear values that should not survive the clone.
func buildCloneInsert(table string, cols []string, overrides map[string]string) string {
	selectCols := make([]string, len(cols))
	for i, c := range cols {
		if v, ok := overrides[c]; ok {
			selectCols[i] = v
		} else {
			selectCols[i] = c
		}
	}
	return fmt.Sprintf(
		"INSERT INTO main.%s (%s) SELECT %s FROM src.%s",
		table,
		strings.Join(cols, ", "),
		strings.Join(selectCols, ", "),
		table,
	)
}

// CloneWorldData copies writs, labels, dependencies, and merge requests
// from the source world database into the target world database. When
// includeHistory is true, agent_history and token_usage are also copied.
// Both databases must already exist (target should be freshly
// created via OpenWorld).
//
// Credentials, tethers, and agent assignments are NOT copied — writs have
// their assignee cleared and merge request claims are reset.
func CloneWorldData(source, target string, includeHistory bool) error {
	srcPath := filepath.Join(config.StoreDir(), source+".db")
	tgtPath := filepath.Join(config.StoreDir(), target+".db")

	tgt, err := open(tgtPath)
	if err != nil {
		return fmt.Errorf("failed to open target world database %q: %w", target, err)
	}
	defer tgt.Close()

	// Attach source database.
	// ATTACH DATABASE does not support parameterized paths — escape single
	// quotes to prevent SQL injection from world names containing them.
	escapedPath := strings.ReplaceAll(srcPath, "'", "''")
	if _, err := tgt.db.Exec(fmt.Sprintf(`ATTACH DATABASE '%s' AS src`, escapedPath)); err != nil {
		return fmt.Errorf("failed to attach source database %q: %w", source, err)
	}
	defer func() {
		if _, detachErr := tgt.db.Exec("DETACH DATABASE src"); detachErr != nil {
			fmt.Fprintf(os.Stderr, "store: failed to detach source database %q: %v\n", source, detachErr)
		}
	}()

	tx, err := tgt.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin clone transaction: %w", err)
	}
	defer tx.Rollback()

	// Copy writs — clear assignee (agents are not cloned).
	if _, err := tx.Exec(buildCloneInsert("writs", cloneWritsColumns, map[string]string{
		"assignee": "NULL",
	})); err != nil {
		return fmt.Errorf("failed to copy writs: %w", err)
	}

	// Copy labels.
	if _, err := tx.Exec(buildCloneInsert("labels", cloneLabelsColumns, nil)); err != nil {
		return fmt.Errorf("failed to copy labels: %w", err)
	}

	// Copy dependencies.
	if _, err := tx.Exec(buildCloneInsert("dependencies", cloneDependenciesColumns, nil)); err != nil {
		return fmt.Errorf("failed to copy dependencies: %w", err)
	}

	// Copy merge requests — clear claims (no agents in new world).
	if _, err := tx.Exec(buildCloneInsert("merge_requests", cloneMergeRequestsColumns, map[string]string{
		"claimed_by": "NULL",
		"claimed_at": "NULL",
		"attempts":   "0",
	})); err != nil {
		return fmt.Errorf("failed to copy merge requests: %w", err)
	}

	if includeHistory {
		// Copy agent history.
		if _, err := tx.Exec(buildCloneInsert("agent_history", cloneAgentHistoryColumns, nil)); err != nil {
			return fmt.Errorf("failed to copy agent history: %w", err)
		}

		// Copy token usage.
		if _, err := tx.Exec(buildCloneInsert("token_usage", cloneTokenUsageColumns, nil)); err != nil {
			return fmt.Errorf("failed to copy token usage: %w", err)
		}
	}

	return tx.Commit()
}
