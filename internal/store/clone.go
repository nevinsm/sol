package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
)

// CloneWorldData copies writs, labels, dependencies, and merge requests
// from the source world database into the target world database. When
// includeHistory is true, agent_history, token_usage, and agent_memories are
// also copied. Both databases must already exist (target should be freshly
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
	if _, err := tx.Exec(`
		INSERT INTO main.writs
			(id, title, description, status, priority, assignee, parent_id, kind, metadata, close_reason, created_by, created_at, updated_at, closed_at)
		SELECT id, title, description, status, priority, NULL, parent_id, kind, metadata, close_reason, created_by, created_at, updated_at, closed_at
		FROM src.writs
	`); err != nil {
		return fmt.Errorf("failed to copy writs: %w", err)
	}

	// Copy labels.
	if _, err := tx.Exec(`
		INSERT INTO main.labels (writ_id, label)
		SELECT writ_id, label FROM src.labels
	`); err != nil {
		return fmt.Errorf("failed to copy labels: %w", err)
	}

	// Copy dependencies.
	if _, err := tx.Exec(`
		INSERT INTO main.dependencies (from_id, to_id)
		SELECT from_id, to_id FROM src.dependencies
	`); err != nil {
		return fmt.Errorf("failed to copy dependencies: %w", err)
	}

	// Copy merge requests — clear claims (no agents in new world).
	if _, err := tx.Exec(`
		INSERT INTO main.merge_requests
			(id, writ_id, branch, phase, claimed_by, claimed_at, attempts, priority, created_at, updated_at, merged_at, blocked_by)
		SELECT id, writ_id, branch, phase, NULL, NULL, 0, priority, created_at, updated_at, merged_at, blocked_by
		FROM src.merge_requests
	`); err != nil {
		return fmt.Errorf("failed to copy merge requests: %w", err)
	}

	if includeHistory {
		// Copy agent history.
		if _, err := tx.Exec(`
			INSERT INTO main.agent_history
				(id, agent_name, writ_id, action, started_at, ended_at, summary)
			SELECT id, agent_name, writ_id, action, started_at, ended_at, summary
			FROM src.agent_history
		`); err != nil {
			return fmt.Errorf("failed to copy agent history: %w", err)
		}

		// Copy token usage.
		if _, err := tx.Exec(`
			INSERT INTO main.token_usage
				(id, history_id, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens)
			SELECT id, history_id, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens
			FROM src.token_usage
		`); err != nil {
			return fmt.Errorf("failed to copy token usage: %w", err)
		}

		// Copy agent memories.
		if _, err := tx.Exec(`
			INSERT INTO main.agent_memories
				(id, agent_name, key, value, created_at)
			SELECT id, agent_name, key, value, created_at
			FROM src.agent_memories
		`); err != nil {
			return fmt.Errorf("failed to copy agent memories: %w", err)
		}
	}

	return tx.Commit()
}
