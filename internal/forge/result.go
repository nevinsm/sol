package forge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ForgeResult is the schema for .forge-result.json written by forge merge sessions.
// The merge session writes this file in the worktree root to report its outcome.
type ForgeResult struct {
	Result       string   `json:"result"`                  // "merged", "failed", or "conflict"
	Summary      string   `json:"summary"`                 // human-readable description of what happened
	FilesChanged []string `json:"files_changed"`           // list of files modified during the merge
	GateOutput   string   `json:"gate_output,omitempty"`   // quality gate stdout/stderr (if relevant)
	NoOp         bool     `json:"no_op,omitempty"`         // true when merge had no changes
}

// resultFileName is the conventional name for the forge result file.
const resultFileName = ".forge-result.json"

// validResults is the set of valid result values.
var validResults = map[string]bool{
	"merged":   true,
	"failed":   true,
	"conflict": true,
}

// ReadResult reads and parses .forge-result.json from the given worktree directory.
// Returns an error if the file doesn't exist, can't be read, or contains invalid JSON.
func ReadResult(worktreeDir string) (*ForgeResult, error) {
	path := filepath.Join(worktreeDir, resultFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("forge result file not found in %q: %w", worktreeDir, err)
		}
		return nil, fmt.Errorf("failed to read forge result file: %w", err)
	}

	var result ForgeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse forge result file: %w", err)
	}

	if err := ValidateResult(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ValidateResult checks that a ForgeResult has valid, well-formed fields.
func ValidateResult(r *ForgeResult) error {
	if r.Result == "" {
		return fmt.Errorf("forge result: missing required field \"result\"")
	}
	if !validResults[r.Result] {
		return fmt.Errorf("forge result: invalid result %q (must be merged, failed, or conflict)", r.Result)
	}
	if r.Summary == "" {
		return fmt.Errorf("forge result: missing required field \"summary\"")
	}
	if r.NoOp && r.Result != "merged" {
		return fmt.Errorf("forge result: no_op is only valid with result \"merged\"")
	}
	return nil
}
