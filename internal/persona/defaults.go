package persona

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

// defaultPersonas embeds every persona template under defaults/. It is the
// single source of truth for which built-in personas are recognized — the
// knownDefaults map below is derived from this FS at init time so adding,
// renaming, or removing a defaults/<name>.md file is automatically reflected.
//
//go:embed defaults/*
var defaultPersonas embed.FS

// knownDefaults lists persona template names that are embedded in the binary.
// Populated at init from defaultPersonas; do not edit by hand.
var knownDefaults map[string]bool

func init() {
	m, err := loadKnownDefaults(defaultPersonas, "defaults")
	if err != nil {
		// The embed FS is baked into the binary; ReadDir on a directory the
		// build embedded should never fail at runtime. If it does, the build
		// itself is malformed — surface that loudly rather than booting with
		// an empty registry.
		panic(fmt.Sprintf("persona: scan embedded defaults: %v", err))
	}
	knownDefaults = m
}

// loadKnownDefaults reads dir on filesys and returns the set of persona names
// it contains (file basenames stripped of the .md suffix). Subdirectories and
// non-.md entries are skipped. Exposed at package scope (unexported) so tests
// can drive it with a fake embed FS via fstest.MapFS without touching the real
// defaultPersonas embed.
func loadKnownDefaults(filesys fs.ReadDirFS, dir string) (map[string]bool, error) {
	entries, err := filesys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		out[strings.TrimSuffix(name, ".md")] = true
	}
	return out, nil
}
