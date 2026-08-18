package guidelines

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readStampsFile is a test helper that reads and decodes the stamp sidecar
// directly (bypassing loadStamps, which tolerates a missing file) so tests
// can assert on its exact on-disk contents.
func readStampsFile(t *testing.T, solHome string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(solHome, "guidelines", ".stamps.json"))
	if err != nil {
		t.Fatalf("failed to read stamp file: %v", err)
	}
	var stamps map[string]string
	if err := json.Unmarshal(data, &stamps); err != nil {
		t.Fatalf("failed to parse stamp file: %v", err)
	}
	return stamps
}

// TestExtractToUserWritesStamp verifies that extracting an embedded default
// to the user tier records its hash in the sidecar stamp file.
func TestExtractToUserWritesStamp(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	embedded, err := readEmbedded("default")
	if err != nil {
		t.Fatalf("readEmbedded: %v", err)
	}

	if _, err := Resolve("default", ""); err != nil {
		t.Fatalf("Resolve(default) error: %v", err)
	}

	stamps := readStampsFile(t, tmpDir)
	got, ok := stamps["default.md"]
	if !ok {
		t.Fatal("expected stamp entry for default.md after extraction")
	}
	if want := hashContent(embedded); got != want {
		t.Errorf("stamp hash = %q, want %q", got, want)
	}
}

// TestRefreshUpToDate covers transition (a): the user-tier file already
// matches the current embedded template. It should be used as-is, and a
// missing/stale stamp should be silently backfilled.
func TestRefreshUpToDate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	embedded, err := readEmbedded("default")
	if err != nil {
		t.Fatalf("readEmbedded: %v", err)
	}
	userPath := userFilePath("default")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, embedded, 0o644); err != nil {
		t.Fatal(err)
	}
	// No stamp on disk yet — simulates a legacy extract that happens to
	// already match the current embedded content.

	res, err := Resolve("default", "")
	if err != nil {
		t.Fatalf("Resolve(default) error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("Tier = %q, want %q", res.Tier, TierUser)
	}
	if string(res.Content) != string(embedded) {
		t.Error("content should match embedded template")
	}

	stamps := readStampsFile(t, tmpDir)
	if got, want := stamps["default.md"], hashContent(embedded); got != want {
		t.Errorf("stamp should be silently backfilled: got %q, want %q", got, want)
	}
}

// TestRefreshUntouchedStaleAutoRefresh covers transition (b): the extract
// was never customized (its hash matches the recorded stamp) but the
// embedded template has since changed. The user-tier file should be
// overwritten with the current embedded content and the stamp updated.
func TestRefreshUntouchedStaleAutoRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	embedded, err := readEmbedded("default")
	if err != nil {
		t.Fatalf("readEmbedded: %v", err)
	}

	// Simulate an old extract: content differs from current embedded, but
	// was stamped at write time with its own (matching) hash.
	oldContent := append([]byte("# Old Embedded Version\n\n"), embedded...)
	userPath := userFilePath("default")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, oldContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveStamps(map[string]string{"default.md": hashContent(oldContent)}); err != nil {
		t.Fatal(err)
	}

	res, err := Resolve("default", "")
	if err != nil {
		t.Fatalf("Resolve(default) error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("Tier = %q, want %q", res.Tier, TierUser)
	}
	if string(res.Content) != string(embedded) {
		t.Error("content should be refreshed to current embedded template")
	}

	// The on-disk file must also have been overwritten.
	onDisk, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(embedded) {
		t.Error("on-disk user-tier file should be overwritten with embedded content")
	}

	stamps := readStampsFile(t, tmpDir)
	if got, want := stamps["default.md"], hashContent(embedded); got != want {
		t.Errorf("stamp should be updated to embedded hash: got %q, want %q", got, want)
	}

	// Refresh must be atomic: no leftover temp files in the guidelines dir.
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(userPath), ".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Errorf("leftover temp file(s) after refresh: %v", matches)
	}
}

// TestRefreshCustomizedPreserved covers transition (c): the extract's hash
// does not match the recorded stamp, meaning the operator customized it.
// The file must be used as-is and never overwritten.
func TestRefreshCustomizedPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	customContent := []byte("# My Customized Guidelines\n")
	userPath := userFilePath("default")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, customContent, 0o644); err != nil {
		t.Fatal(err)
	}
	// Stamp records a *different* hash — as if it were originally extracted
	// with different content and then hand-edited.
	if err := saveStamps(map[string]string{"default.md": hashContent([]byte("# Original Extract\n"))}); err != nil {
		t.Fatal(err)
	}

	res, err := Resolve("default", "")
	if err != nil {
		t.Fatalf("Resolve(default) error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("Tier = %q, want %q", res.Tier, TierUser)
	}
	if string(res.Content) != string(customContent) {
		t.Error("customized content must be returned as-is")
	}

	onDisk, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(customContent) {
		t.Error("customized file must never be overwritten")
	}

	stamps := readStampsFile(t, tmpDir)
	if got := stamps["default.md"]; got != hashContent([]byte("# Original Extract\n")) {
		t.Errorf("stamp must be left untouched for customized extracts, got %q", got)
	}
}

// TestRefreshLegacyUnstamped covers transition (d): a pre-stamping extract
// with no sidecar entry at all. Both branches: content that happens to
// match the embedded template (silently stamped, case a) and content that
// differs (treated as customized/unverifiable, left alone).
func TestRefreshLegacyUnstamped(t *testing.T) {
	t.Run("matches embedded", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("SOL_HOME", tmpDir)

		embedded, err := readEmbedded("default")
		if err != nil {
			t.Fatalf("readEmbedded: %v", err)
		}
		userPath := userFilePath("default")
		if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userPath, embedded, 0o644); err != nil {
			t.Fatal(err)
		}
		// No .stamps.json on disk at all.

		res, err := Resolve("default", "")
		if err != nil {
			t.Fatalf("Resolve(default) error: %v", err)
		}
		if string(res.Content) != string(embedded) {
			t.Error("content should match embedded template")
		}
		stamps := readStampsFile(t, tmpDir)
		if got, want := stamps["default.md"], hashContent(embedded); got != want {
			t.Errorf("legacy unstamped-but-matching extract should be silently stamped: got %q, want %q", got, want)
		}
	})

	t.Run("differs from embedded", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("SOL_HOME", tmpDir)

		customContent := []byte("# Legacy Pre-Stamp Customization\n")
		userPath := userFilePath("default")
		if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userPath, customContent, 0o644); err != nil {
			t.Fatal(err)
		}
		// No .stamps.json on disk at all.

		res, err := Resolve("default", "")
		if err != nil {
			t.Fatalf("Resolve(default) error: %v", err)
		}
		if string(res.Content) != string(customContent) {
			t.Error("legacy unstamped-and-differing extract must be used as-is")
		}
		onDisk, err := os.ReadFile(userPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(onDisk) != string(customContent) {
			t.Error("legacy unstamped-and-differing extract must never be overwritten")
		}

		// No stamp should have been fabricated for unverifiable content —
		// CheckStaleExtracts relies on its absence to report it correctly.
		if _, err := os.Stat(filepath.Join(tmpDir, "guidelines", ".stamps.json")); !os.IsNotExist(err) {
			t.Error("no stamp file should be created for unverifiable legacy content")
		}
	})
}

// TestCheckStaleExtracts exercises guidelines.CheckStaleExtracts across all
// the states doctor needs to distinguish.
func TestCheckStaleExtracts(t *testing.T) {
	embedded, err := readEmbedded("default")
	if err != nil {
		t.Fatalf("readEmbedded: %v", err)
	}

	t.Run("no extracts", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("SOL_HOME", tmpDir)

		stale, err := CheckStaleExtracts()
		if err != nil {
			t.Fatalf("CheckStaleExtracts: %v", err)
		}
		if len(stale) != 0 {
			t.Errorf("expected no stale extracts, got %v", stale)
		}
	})

	t.Run("up to date is not stale", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("SOL_HOME", tmpDir)

		userPath := userFilePath("default")
		if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userPath, embedded, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := saveStamps(map[string]string{"default.md": hashContent(embedded)}); err != nil {
			t.Fatal(err)
		}

		stale, err := CheckStaleExtracts()
		if err != nil {
			t.Fatalf("CheckStaleExtracts: %v", err)
		}
		if len(stale) != 0 {
			t.Errorf("up-to-date extract should not be reported stale, got %v", stale)
		}
	})

	t.Run("untouched-stale self-heals, not reported", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("SOL_HOME", tmpDir)

		oldContent := append([]byte("# Old\n\n"), embedded...)
		userPath := userFilePath("default")
		if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userPath, oldContent, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := saveStamps(map[string]string{"default.md": hashContent(oldContent)}); err != nil {
			t.Fatal(err)
		}

		stale, err := CheckStaleExtracts()
		if err != nil {
			t.Fatalf("CheckStaleExtracts: %v", err)
		}
		if len(stale) != 0 {
			t.Errorf("untouched-stale extract auto-refreshes on next use, should not be reported, got %v", stale)
		}
	})

	t.Run("customized is stale and verifiable", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("SOL_HOME", tmpDir)

		customContent := []byte("# Customized\n")
		userPath := userFilePath("default")
		if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userPath, customContent, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := saveStamps(map[string]string{"default.md": hashContent([]byte("# Original\n"))}); err != nil {
			t.Fatal(err)
		}

		stale, err := CheckStaleExtracts()
		if err != nil {
			t.Fatalf("CheckStaleExtracts: %v", err)
		}
		if len(stale) != 1 {
			t.Fatalf("expected 1 stale extract, got %d: %v", len(stale), stale)
		}
		if !stale[0].HasStamp || !stale[0].Verifiable {
			t.Errorf("customized extract should be HasStamp=true, Verifiable=true, got %+v", stale[0])
		}
		if stale[0].Name != "default" {
			t.Errorf("Name = %q, want %q", stale[0].Name, "default")
		}
	})

	t.Run("legacy unstamped differing is stale and unverifiable", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("SOL_HOME", tmpDir)

		customContent := []byte("# Legacy\n")
		userPath := userFilePath("default")
		if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userPath, customContent, 0o644); err != nil {
			t.Fatal(err)
		}

		stale, err := CheckStaleExtracts()
		if err != nil {
			t.Fatalf("CheckStaleExtracts: %v", err)
		}
		if len(stale) != 1 {
			t.Fatalf("expected 1 stale extract, got %d: %v", len(stale), stale)
		}
		if stale[0].HasStamp || stale[0].Verifiable {
			t.Errorf("legacy unstamped extract should be HasStamp=false, Verifiable=false, got %+v", stale[0])
		}
	})
}
