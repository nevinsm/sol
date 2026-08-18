package workflow

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/stamp"
)

// stampsFileName is the sidecar recording the sha256 hash of each file in an
// auto-extracted workflow directory at the time it was last written by sol
// (initial extraction, or a later per-file refresh). It lives inside the
// extracted directory itself, alongside embeddedVersionFile — unlike
// guidelines' single sphere-wide sidecar, each auto-extracted workflow
// directory is already self-contained (it owns embeddedVersionFile), so a
// directory-local sidecar keeps stamps colocated with the content they
// describe and needs no name-spacing key. Keys are slash-separated paths
// relative to the workflow directory (e.g. "manifest.toml",
// "steps/01-start.md"), so it stays plain, cat-able JSON like the
// guidelines sidecar.
const stampsFileName = ".stamps.json"

func stampsFilePath(workflowDir string) string {
	return filepath.Join(workflowDir, stampsFileName)
}

// embeddedFileMap reads every file in the named embedded workflow into a map
// keyed by slash-separated path relative to the workflow's embedded root.
func embeddedFileMap(name string) (map[string][]byte, error) {
	root := filepath.Join("defaults", name)
	files := map[string][]byte{}
	err := fs.WalkDir(defaultWorkflows, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := defaultWorkflows.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %q: %w", path, err)
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk embedded workflow %q: %w", name, err)
	}
	return files, nil
}

// diskFileList lists every regular file inside workflowDir, relative to it
// (slash-separated), excluding sol's own metadata files (the version marker
// and the stamp sidecar) which are not part of the workflow's content.
func diskFileList(workflowDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(workflowDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(workflowDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == embeddedVersionFile || rel == stampsFileName {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files in workflow directory %q: %w", workflowDir, err)
	}
	sort.Strings(files)
	return files, nil
}

// fileClass classifies one file inside an auto-extracted workflow directory
// relative to the current embedded template and the recorded stamp.
type fileClass int

const (
	// classUpToDate: disk content already matches the current embedded file.
	classUpToDate fileClass = iota
	// classRefreshable: disk == recorded stamp, but != current embedded —
	// untouched since extraction/last refresh; safe to overwrite.
	classRefreshable
	// classCustomized: disk differs from both the current embedded content
	// and the recorded stamp (or has no stamp at all) — hand-edited, or
	// predates stamping and can't be verified as untouched. Preserve.
	classCustomized
	// classObsoleteUntouched: no longer part of the embedded set, and disk
	// == recorded stamp — safe to delete now that upstream dropped it.
	classObsoleteUntouched
	// classObsoleteCustomized: no longer part of the embedded set, and disk
	// content isn't confirmed untouched — preserve, flag as drifted.
	classObsoleteCustomized
	// classNew: present in the embedded set but missing on disk — extract
	// fresh.
	classNew
)

// fileDiff is the outcome of the three-way comparison (disk vs. current
// embedded vs. recorded stamp) for one file relative to a workflow
// directory.
type fileDiff struct {
	RelPath  string
	Class    fileClass
	Disk     []byte // disk content; nil for classNew
	Embedded []byte // current embedded content; nil for the obsolete classes
}

// diffWorkflowFiles computes the per-file three-way comparison between the
// current embedded content of an embedded workflow and what's on disk at
// workflowDir, without mutating anything. It is the single source of truth
// for classification, shared by refreshExtractedWorkflow (which applies the
// classification) and CheckStaleFiles (which only reports on it) so the two
// can never disagree about what counts as "customized".
//
// Returns the diffs (in deterministic, lexically-sorted-by-path order for
// on-disk files, followed by any classNew entries) plus the stamp map that
// was loaded to compute them — callers that mutate reuse this map rather
// than reloading it.
func diffWorkflowFiles(name, workflowDir string) ([]fileDiff, map[string]string, error) {
	embeddedFiles, err := embeddedFileMap(name)
	if err != nil {
		return nil, nil, err
	}

	stampsPath := stampsFilePath(workflowDir)
	stamps, err := stamp.Load(stampsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load workflow stamp file %q: %w", stampsPath, err)
	}

	diskFiles, err := diskFileList(workflowDir)
	if err != nil {
		return nil, nil, err
	}

	seen := make(map[string]bool, len(embeddedFiles))
	var diffs []fileDiff

	for _, rel := range diskFiles {
		diskPath := filepath.Join(workflowDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(diskPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read workflow file %q: %w", diskPath, err)
		}
		diskHash := stamp.Hash(data)

		embeddedData, stillEmbedded := embeddedFiles[rel]
		if stillEmbedded {
			seen[rel] = true
			embeddedHash := stamp.Hash(embeddedData)

			if diskHash == embeddedHash {
				diffs = append(diffs, fileDiff{RelPath: rel, Class: classUpToDate, Disk: data, Embedded: embeddedData})
				continue
			}

			stampedHash, hasStamp := stamps[rel]
			if hasStamp && stampedHash == diskHash {
				diffs = append(diffs, fileDiff{RelPath: rel, Class: classRefreshable, Disk: data, Embedded: embeddedData})
				continue
			}

			diffs = append(diffs, fileDiff{RelPath: rel, Class: classCustomized, Disk: data, Embedded: embeddedData})
			continue
		}

		// No longer part of the embedded set.
		stampedHash, hasStamp := stamps[rel]
		if hasStamp && stampedHash == diskHash {
			diffs = append(diffs, fileDiff{RelPath: rel, Class: classObsoleteUntouched, Disk: data})
			continue
		}
		diffs = append(diffs, fileDiff{RelPath: rel, Class: classObsoleteCustomized, Disk: data})
	}

	// Files newly added to the embedded set since this directory was last
	// extracted/refreshed. Order is nondeterministic (map iteration) but
	// that only matters for extraction side effects, not for reporting —
	// CheckStaleFiles never surfaces classNew entries.
	for rel, data := range embeddedFiles {
		if seen[rel] {
			continue
		}
		diffs = append(diffs, fileDiff{RelPath: rel, Class: classNew, Embedded: data})
	}

	return diffs, stamps, nil
}

// refreshExtractedWorkflow reconciles an auto-extracted workflow directory
// with the current embedded content, file by file, in place of the old
// behavior (delete the whole directory and re-extract it) that silently
// destroyed operator hand-edits made without going through Eject.
//
//   - Untouched files (disk == recorded stamp, != current embedded) are
//     refreshed transparently and re-stamped.
//   - Files that already match current embedded are (re)stamped, covering
//     legacy directories that predate this file's stamp sidecar.
//   - Hand-edited files (or files that predate stamping and can't be
//     verified as untouched) are preserved exactly as-is — never
//     overwritten, never deleted.
//   - Files removed from the embedded set upstream are deleted only if
//     untouched; hand-edited ones are preserved (doctor's CheckStaleFiles
//     flags them as drifted).
//   - Files newly added to the embedded set upstream are extracted fresh.
//
// Called from Resolve when the directory-level version marker indicates the
// embedded template has moved on since this directory was last
// extracted/refreshed.
func refreshExtractedWorkflow(name, workflowDir string) error {
	diffs, stamps, err := diffWorkflowFiles(name, workflowDir)
	if err != nil {
		return err
	}

	for _, d := range diffs {
		dest := filepath.Join(workflowDir, filepath.FromSlash(d.RelPath))
		switch d.Class {
		case classUpToDate:
			stamps[d.RelPath] = stamp.Hash(d.Disk)

		case classRefreshable:
			if err := fileutil.AtomicWrite(dest, d.Embedded, 0o644); err != nil {
				return fmt.Errorf("failed to refresh workflow file %q: %w", dest, err)
			}
			stamps[d.RelPath] = stamp.Hash(d.Embedded)
			slog.Info("workflow: refreshed untouched auto-extracted file to match updated embedded template",
				"workflow", name, "file", d.RelPath)

		case classCustomized:
			// Hand-edited, or unverifiable legacy content — preserve as-is.
			// Leave any existing stamp entry untouched too: it still
			// describes the last content sol itself wrote, which remains
			// meaningful context even though it no longer matches disk.

		case classObsoleteUntouched:
			if err := os.Remove(dest); err != nil {
				return fmt.Errorf("failed to remove obsolete workflow file %q: %w", dest, err)
			}
			delete(stamps, d.RelPath)

		case classObsoleteCustomized:
			// No longer part of the embedded set, but not confirmed
			// untouched — preserve; doctor flags it as drifted.

		case classNew:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return fmt.Errorf("failed to create directory for new workflow file %q: %w", dest, err)
			}
			if err := fileutil.AtomicWrite(dest, d.Embedded, 0o644); err != nil {
				return fmt.Errorf("failed to extract new workflow file %q: %w", dest, err)
			}
			stamps[d.RelPath] = stamp.Hash(d.Embedded)
		}
	}

	if err := stamp.Save(stampsFilePath(workflowDir), stamps); err != nil {
		return fmt.Errorf("failed to save workflow stamp file: %w", err)
	}

	return writeVersionMarker(name, workflowDir)
}

// stampAllFiles records the hash of every embedded file for name in the
// stamp sidecar at workflowDir. Called right after a fresh extraction
// (extractEmbedded) so every file starts life correctly stamped. A failure
// here doesn't fail the extraction itself — worst case those files are
// later treated as unverifiable legacy content, same as any pre-stamping
// directory, and CheckStaleFiles surfaces them for the operator.
func stampAllFiles(name, workflowDir string) error {
	embeddedFiles, err := embeddedFileMap(name)
	if err != nil {
		return err
	}
	stamps := make(map[string]string, len(embeddedFiles))
	for rel, data := range embeddedFiles {
		stamps[rel] = stamp.Hash(data)
	}
	return stamp.Save(stampsFilePath(workflowDir), stamps)
}

// StaleFile describes a file inside an auto-extracted workflow directory
// that diverges from its current embedded template in a way that cannot be
// safely auto-refreshed: it was either hand-edited by the operator (without
// going through Eject), or predates per-file stamping and can't be verified
// as untouched.
type StaleFile struct {
	WorkflowName string // embedded workflow name, e.g. "code-review"
	RelPath      string // slash-separated path relative to the workflow directory
	Path         string // full path to the file on disk
	HasStamp     bool   // false for legacy pre-stamping directories
	Verifiable   bool   // true if this is confirmed drift (stamp mismatch) rather than merely unverifiable (no stamp on record)
}

// CheckStaleFiles scans every auto-extracted known-default workflow
// directory ($SOL_HOME/workflows/{name}/, identified by the presence of
// embeddedVersionFile) and returns files `sol doctor` should flag: content
// that differs from the current embedded template and whose stamp does not
// confirm it was last written to match it.
//
// Files that are up to date, or untouched and would be auto-refreshed on
// the next Resolve, are not returned — there is nothing for an operator to
// act on. Directories with no version marker are user-owned (ejected or
// hand-created) and are out of scope entirely.
func CheckStaleFiles() ([]StaleFile, error) {
	names := make([]string, 0, len(knownDefaults))
	for name := range knownDefaults {
		names = append(names, name)
	}
	sort.Strings(names)

	var stale []StaleFile
	for _, name := range names {
		workflowDir := Dir(name)
		if _, err := os.Stat(filepath.Join(workflowDir, embeddedVersionFile)); err != nil {
			continue // not auto-extracted — nothing to check
		}

		diffs, stamps, err := diffWorkflowFiles(name, workflowDir)
		if err != nil {
			return nil, fmt.Errorf("failed to check workflow %q for drift: %w", name, err)
		}

		for _, d := range diffs {
			if d.Class != classCustomized && d.Class != classObsoleteCustomized {
				continue
			}
			_, hasStamp := stamps[d.RelPath]
			stale = append(stale, StaleFile{
				WorkflowName: name,
				RelPath:      d.RelPath,
				Path:         filepath.Join(workflowDir, filepath.FromSlash(d.RelPath)),
				HasStamp:     hasStamp,
				Verifiable:   hasStamp,
			})
		}
	}
	return stale, nil
}
