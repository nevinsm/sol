package guidelines

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// stampsFilePath returns the path to the sidecar stamp file that records the
// sha256 hash of each user-tier extract at the time it was last written by
// sol (either the initial extractToUser write, or a later auto-refresh).
//
// This is a sidecar rather than an in-file header because guideline content
// is injected verbatim into agent prompts and must stay clean. It is plain,
// flat JSON (name.md -> hex sha256) so it stays cat-able (GLASS).
func stampsFilePath() string {
	return filepath.Join(config.Home(), "guidelines", ".stamps.json")
}

// stampKey returns the sidecar map key for a guidelines name — the extract's
// base filename, matching the last path element of userFilePath.
func stampKey(name string) string {
	return name + ".md"
}

// hashContent returns the hex-encoded sha256 of data.
func hashContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// loadStamps reads the stamp sidecar. A missing file is not an error — it
// returns an empty map, which covers fresh installs and spheres that
// predate this feature.
func loadStamps() (map[string]string, error) {
	data, err := os.ReadFile(stampsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to read guidelines stamp file %q: %w", stampsFilePath(), err)
	}
	stamps := map[string]string{}
	if err := json.Unmarshal(data, &stamps); err != nil {
		return nil, fmt.Errorf("failed to parse guidelines stamp file %q: %w", stampsFilePath(), err)
	}
	return stamps, nil
}

// saveStamps writes the stamp sidecar atomically (temp file + rename, per
// fileutil conventions).
func saveStamps(stamps map[string]string) error {
	path := stampsFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create guidelines directory %q: %w", filepath.Dir(path), err)
	}
	if err := fileutil.AtomicWriteJSON(path, stamps, 0o644); err != nil {
		return fmt.Errorf("failed to write guidelines stamp file %q: %w", path, err)
	}
	return nil
}

// stampExtract records the hash of newly-written extract content in the
// sidecar. Called by extractToUser right after a successful write. Errors
// are the caller's to handle — extraction itself already succeeded, so a
// stamp failure should not fail the extraction, only be logged.
func stampExtract(name string, data []byte) error {
	stamps, err := loadStamps()
	if err != nil {
		return err
	}
	stamps[stampKey(name)] = hashContent(data)
	return saveStamps(stamps)
}

// ensureStamp writes the stamp for key if it is missing or does not already
// match hash. Used to silently backfill stamps for extracts that are found
// to be up to date with the embedded template (case (a) in refreshUserExtract,
// including legacy pre-stamp extracts that happen to already match).
func ensureStamp(key, hash string) {
	stamps, err := loadStamps()
	if err != nil {
		slog.Warn("guidelines: failed to load stamp file", "error", err)
		return
	}
	if stamps[key] == hash {
		return
	}
	stamps[key] = hash
	if err := saveStamps(stamps); err != nil {
		slog.Warn("guidelines: failed to write stamp", "key", key, "error", err)
	}
}

// refreshUserExtract implements the auto-refresh policy for a known-default
// user-tier extract at resolution time:
//
//   - file hash == current embedded hash: up to date; use as-is. If the
//     stamp is missing or stale, (re)write it silently — this also covers
//     legacy pre-stamp extracts that happen to already match the embedded
//     template.
//   - file hash == recorded stamp, but != current embedded hash: the extract
//     was never customized and the embedded template has since moved on.
//     Overwrite the user-tier file with the current embedded content, update
//     the stamp, log at INFO, and return the refreshed content.
//   - otherwise (stamp mismatch, or no stamp and hashes differ): the operator
//     customized the extract, or it predates stamping and can't be verified
//     as untouched. Leave it as-is; never overwrite. `sol doctor` surfaces
//     this case as a remediation hint (see CheckStaleExtracts).
//
// Returns the content callers should use for this resolution: either the
// original file content, or freshly refreshed embedded content.
func refreshUserExtract(name, userPath string, fileData []byte) []byte {
	embeddedData, err := readEmbedded(name)
	if err != nil {
		// Can't compare without the embedded source — use the file as-is.
		slog.Warn("guidelines: failed to read embedded template for refresh check", "name", name, "error", err)
		return fileData
	}

	fileHash := hashContent(fileData)
	embeddedHash := hashContent(embeddedData)
	key := stampKey(name)

	if fileHash == embeddedHash {
		ensureStamp(key, fileHash)
		return fileData
	}

	stamps, err := loadStamps()
	if err != nil {
		slog.Warn("guidelines: failed to load stamp file", "name", name, "error", err)
		return fileData
	}

	stampedHash, hasStamp := stamps[key]
	if !hasStamp || stampedHash != fileHash {
		// Customized, or an unverifiable legacy extract — never overwrite.
		return fileData
	}

	// Untouched since extraction, and the embedded template has moved on:
	// refresh the user-tier copy so future embedded updates keep reaching it.
	if err := fileutil.AtomicWrite(userPath, embeddedData, 0o644); err != nil {
		slog.Warn("guidelines: failed to refresh stale user-tier extract", "name", name, "error", err)
		return fileData
	}
	stamps[key] = embeddedHash
	if err := saveStamps(stamps); err != nil {
		slog.Warn("guidelines: failed to update stamp after refresh", "name", name, "error", err)
	}
	slog.Info("guidelines: refreshed untouched user-tier extract to match updated embedded template",
		"name", name, "path", userPath)
	return embeddedData
}

// StaleExtract describes a user-tier guidelines extract that diverges from
// its current embedded template in a way that cannot be safely
// auto-refreshed: it was either customized by the operator, or predates
// stamping and can't be verified as untouched.
type StaleExtract struct {
	Name       string // guidelines template name, e.g. "default"
	Path       string // path to the user-tier file
	HasStamp   bool   // false for legacy pre-stamp extracts
	Verifiable bool   // true if this is confirmed customization (stamp mismatch) rather than merely unverifiable (no stamp on record)
}

// CheckStaleExtracts scans every known-default user-tier guideline extract
// and returns those `sol doctor` should flag: files whose content differs
// from the current embedded template and whose stamp does not confirm they
// were last written to match it.
//
// Extracts that don't exist on disk, are up to date, or are untouched and
// would be auto-refreshed on next use (guidelines.Resolve's case (b)) are
// not returned — there is nothing for an operator to act on.
func CheckStaleExtracts() ([]StaleExtract, error) {
	stamps, err := loadStamps()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(knownDefaults))
	for name := range knownDefaults {
		names = append(names, name)
	}
	sort.Strings(names)

	var stale []StaleExtract
	for _, name := range names {
		userPath := userFilePath(name)
		data, err := os.ReadFile(userPath)
		if err != nil {
			continue // not extracted — nothing to check
		}
		embedded, err := readEmbedded(name)
		if err != nil {
			continue // shouldn't happen for a known default
		}

		fileHash := hashContent(data)
		embeddedHash := hashContent(embedded)
		if fileHash == embeddedHash {
			continue // up to date
		}

		stampedHash, hasStamp := stamps[stampKey(name)]
		if hasStamp && fileHash == stampedHash {
			continue // untouched — self-heals on next Resolve
		}

		stale = append(stale, StaleExtract{
			Name:       name,
			Path:       userPath,
			HasStamp:   hasStamp,
			Verifiable: hasStamp,
		})
	}
	return stale, nil
}
