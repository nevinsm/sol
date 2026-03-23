package worldexport

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// ImportOptions controls import behavior.
type ImportOptions struct {
	ArchivePath string // path to the .tar.gz archive
	Name        string // override world name (empty = use archive name)
}

// ImportResult describes the outcome of an import.
type ImportResult struct {
	World      string // imported world name
	SourceRepo string // source_repo from world.toml (for sync hint)
}

// Import restores a world from an export archive.
func Import(opts ImportOptions) (*ImportResult, error) {
	// 1. Extract archive to temp dir.
	tmpDir, err := os.MkdirTemp("", "sol-import-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractArchive(opts.ArchivePath, tmpDir); err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	// Find the archive root directory (the single top-level entry).
	archiveRoot, err := findArchiveRoot(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find archive root: %w", err)
	}

	// 2. Read and validate manifest.
	manifest, err := readManifest(archiveRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read archive manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid archive: %w", err)
	}

	// Determine target world name.
	worldName := manifest.World
	if opts.Name != "" {
		worldName = opts.Name
	}

	if err := config.ValidateWorldName(worldName); err != nil {
		return nil, fmt.Errorf("failed to validate world name: %w", err)
	}

	// 3. Check world name conflict.
	tomlPath := config.WorldConfigPath(worldName)
	if _, err := os.Stat(tomlPath); err == nil {
		return nil, fmt.Errorf("world %q already exists; delete it first or use --name to import under a different name", worldName)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check world config %q: %w", tomlPath, err)
	}

	// Also check if the DB already exists (pre-Arc1 world).
	dbDst := filepath.Join(config.StoreDir(), worldName+".db")
	if _, err := os.Stat(dbDst); err == nil {
		return nil, fmt.Errorf("world database %q already exists; delete it first or use --name to import under a different name", worldName)
	}

	// 4. Create world directory structure.
	worldDir := config.WorldDir(worldName)
	if err := os.MkdirAll(filepath.Join(worldDir, "outposts"), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create world directory: %w", err)
	}
	// Create role-specific directories based on agent records in the archive.
	if err := createAgentDirs(archiveRoot, worldName); err != nil {
		os.RemoveAll(worldDir)
		return nil, fmt.Errorf("failed to create agent directories: %w", err)
	}

	// 5. Copy world.db into .store/{name}.db.
	if err := config.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	dbSrc := filepath.Join(archiveRoot, "world.db")
	if _, err := os.Stat(dbSrc); err != nil {
		return nil, fmt.Errorf("archive is missing world.db: %w", err)
	}
	if err := copyFile(dbSrc, dbDst); err != nil {
		return nil, fmt.Errorf("failed to copy world database: %w", err)
	}

	// Open the world DB to trigger migration (brings older schemas up to date).
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		// Clean up the copied DB on failure.
		os.Remove(dbDst)
		os.RemoveAll(worldDir)
		return nil, fmt.Errorf("failed to open imported world database: %w", err)
	}
	worldStore.Close()

	// 6. Copy world.toml.
	tomlSrc := filepath.Join(archiveRoot, "world.toml")
	if _, err := os.Stat(tomlSrc); err != nil {
		// Clean up on failure.
		os.Remove(dbDst)
		os.RemoveAll(worldDir)
		return nil, fmt.Errorf("archive is missing world.toml: %w", err)
	}
	if err := copyFile(tomlSrc, tomlPath); err != nil {
		os.Remove(dbDst)
		os.RemoveAll(worldDir)
		return nil, fmt.Errorf("failed to copy world config: %w", err)
	}

	// Read source_repo from the imported config for the result.
	cfg, err := config.LoadWorldConfig(worldName)
	if err != nil {
		// Non-fatal — continue with empty source_repo.
		cfg = config.WorldConfig{}
	}

	// 7. Register in sphere.db.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		os.Remove(dbDst)
		os.RemoveAll(worldDir)
		return nil, fmt.Errorf("failed to open sphere database: %w", err)
	}
	defer sphereStore.Close()

	if err := sphereStore.RegisterWorld(worldName, cfg.World.SourceRepo); err != nil {
		os.Remove(dbDst)
		os.RemoveAll(worldDir)
		return nil, fmt.Errorf("failed to register world: %w", err)
	}

	// 8. Insert sphere-scoped data.
	renaming := opts.Name != "" && opts.Name != manifest.World
	if err := importSphereData(sphereStore, archiveRoot, manifest.World, worldName, renaming); err != nil {
		// Best-effort cleanup: delete the world data we just inserted.
		sphereStore.DeleteWorldData(worldName)
		os.Remove(dbDst)
		os.RemoveAll(worldDir)
		return nil, fmt.Errorf("failed to import sphere data: %w", err)
	}

	// 9. Restore writ-outputs/ directory (if present in archive).
	writOutputsSrc := filepath.Join(archiveRoot, "writ-outputs")
	if info, err := os.Stat(writOutputsSrc); err == nil && info.IsDir() {
		writOutputsDst := filepath.Join(worldDir, "writ-outputs")
		if err := copyDir(writOutputsSrc, writOutputsDst); err != nil {
			return nil, fmt.Errorf("failed to restore writ-outputs: %w", err)
		}
	}

	return &ImportResult{
		World:      worldName,
		SourceRepo: cfg.World.SourceRepo,
	}, nil
}

// importSphereData reads JSON files from the sphere-data/ directory and
// inserts them into sphere.db. If renaming is true, agent IDs and caravan
// item world references are rewritten from oldWorld to newWorld.
func importSphereData(s *store.SphereStore, archiveRoot, oldWorld, newWorld string, renaming bool) error {
	sphereDir := filepath.Join(archiveRoot, "sphere-data")
	if _, err := os.Stat(sphereDir); os.IsNotExist(err) {
		// No sphere data to import — not an error.
		return nil
	}

	// Import agents.
	if err := importAgents(s, sphereDir, oldWorld, newWorld, renaming); err != nil {
		return fmt.Errorf("failed to import agents: %w", err)
	}

	// Import messages.
	if err := importMessages(s, sphereDir, oldWorld, newWorld, renaming); err != nil {
		return fmt.Errorf("failed to import messages: %w", err)
	}

	// Import escalations.
	if err := importEscalations(s, sphereDir, oldWorld, newWorld, renaming); err != nil {
		return fmt.Errorf("failed to import escalations: %w", err)
	}

	// Import caravans.
	if err := importCaravans(s, sphereDir); err != nil {
		return fmt.Errorf("failed to import caravans: %w", err)
	}

	// Import caravan items.
	if err := importCaravanItems(s, sphereDir, oldWorld, newWorld, renaming); err != nil {
		return fmt.Errorf("failed to import caravan items: %w", err)
	}

	return nil
}

func importAgents(s *store.SphereStore, sphereDir, oldWorld, newWorld string, renaming bool) error {
	var agents []ExportAgent
	if err := readJSONFile(filepath.Join(sphereDir, "agents.json"), &agents); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read agents.json: %w", err)
	}

	for _, a := range agents {
		id := a.ID
		world := a.World
		if renaming {
			id = rewriteAgentID(id, oldWorld, newWorld)
			world = newWorld
		}
		if err := s.ImportAgent(id, a.Name, world, a.Role, a.CreatedAt, a.UpdatedAt); err != nil {
			return fmt.Errorf("failed to import agent %q: %w", a.Name, err)
		}
	}
	return nil
}

func importMessages(s *store.SphereStore, sphereDir, oldWorld, newWorld string, renaming bool) error {
	var messages []ExportMessage
	if err := readJSONFile(filepath.Join(sphereDir, "messages.json"), &messages); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read messages.json: %w", err)
	}

	for _, m := range messages {
		sender := m.Sender
		recipient := m.Recipient
		if renaming {
			sender = rewriteAgentID(sender, oldWorld, newWorld)
			recipient = rewriteAgentID(recipient, oldWorld, newWorld)
		}
		if err := s.ImportMessage(m.ID, sender, recipient, m.Subject, m.Body,
			m.Priority, m.Type, m.ThreadID, m.Delivery, m.Read, m.CreatedAt, m.AckedAt); err != nil {
			return fmt.Errorf("failed to import message %q: %w", m.ID, err)
		}
	}
	return nil
}

func importEscalations(s *store.SphereStore, sphereDir, oldWorld, newWorld string, renaming bool) error {
	var escalations []ExportEscalation
	if err := readJSONFile(filepath.Join(sphereDir, "escalations.json"), &escalations); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read escalations.json: %w", err)
	}

	for _, e := range escalations {
		source := e.Source
		if renaming {
			source = rewriteAgentID(source, oldWorld, newWorld)
		}
		if err := s.ImportEscalation(e.ID, e.Severity, source, e.Description,
			e.Status, e.Acknowledged, e.CreatedAt, e.UpdatedAt, e.SourceRef, e.LastNotifiedAt); err != nil {
			return fmt.Errorf("failed to import escalation %q: %w", e.ID, err)
		}
	}
	return nil
}

func importCaravans(s *store.SphereStore, sphereDir string) error {
	var caravans []ExportCaravan
	if err := readJSONFile(filepath.Join(sphereDir, "caravans.json"), &caravans); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read caravans.json: %w", err)
	}

	for _, c := range caravans {
		if err := s.ImportCaravan(c.ID, c.Name, c.Status, c.Owner, c.CreatedAt, c.ClosedAt); err != nil {
			return fmt.Errorf("failed to import caravan %q: %w", c.ID, err)
		}
	}
	return nil
}

func importCaravanItems(s *store.SphereStore, sphereDir, oldWorld, newWorld string, renaming bool) error {
	var items []ExportCaravanItem
	if err := readJSONFile(filepath.Join(sphereDir, "caravan_items.json"), &items); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read caravan_items.json: %w", err)
	}

	for _, ci := range items {
		world := ci.World
		if renaming && ci.World == oldWorld {
			world = newWorld
		}
		if err := s.ImportCaravanItem(ci.CaravanID, ci.WritID, world, ci.Phase); err != nil {
			return fmt.Errorf("failed to import caravan item %q: %w", ci.WritID, err)
		}
	}
	return nil
}

// rewriteAgentID replaces agent IDs of the form "oldWorld/name" with "newWorld/name".
// Non-matching IDs are returned unchanged (e.g., cross-world references).
func rewriteAgentID(id, oldWorld, newWorld string) string {
	prefix := oldWorld + "/"
	if strings.HasPrefix(id, prefix) {
		return newWorld + "/" + strings.TrimPrefix(id, prefix)
	}
	return id
}

// extractArchive extracts a .tar.gz archive into the destination directory.
func extractArchive(archivePath, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive %q: %w", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Security: prevent path traversal.
		target := filepath.Join(dst, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dst)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(dst) {
			return fmt.Errorf("archive contains path traversal: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %q: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("failed to create parent directory for %q: %w", target, err)
			}
			out, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("failed to create file %q: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("failed to write file %q: %w", target, err)
			}
			out.Close()
		}
	}
	return nil
}

// findArchiveRoot locates the single top-level directory in the extraction.
// Export archives always have a top-level directory like sol-export-{name}-{timestamp}/.
func findArchiveRoot(tmpDir string) (string, error) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", fmt.Errorf("failed to read temp directory: %w", err)
	}

	// Look for a directory entry.
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(tmpDir, e.Name()), nil
		}
	}

	// No subdirectory found — files may be at top level (flat archive).
	// Check if manifest.json exists at the top level.
	if _, err := os.Stat(filepath.Join(tmpDir, "manifest.json")); err == nil {
		return tmpDir, nil
	}

	return "", fmt.Errorf("archive does not contain a valid export directory or manifest.json")
}

// readManifest reads and parses manifest.json from the archive root.
func readManifest(archiveRoot string) (*Manifest, error) {
	path := filepath.Join(archiveRoot, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest.json: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest.json: %w", err)
	}
	return &m, nil
}

// readJSONFile reads and unmarshals a JSON file into the given target.
func readJSONFile(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to parse %s: %w", filepath.Base(path), err)
	}
	return nil
}

// createAgentDirs reads agents.json from the archive and creates the
// role-specific directories that each agent requires (envoys/, governors/, outposts/).
func createAgentDirs(archiveRoot, worldName string) error {
	sphereDir := filepath.Join(archiveRoot, "sphere-data")
	agentsFile := filepath.Join(sphereDir, "agents.json")

	var agents []ExportAgent
	if err := readJSONFile(agentsFile, &agents); err != nil {
		if os.IsNotExist(err) {
			return nil // No agents to create dirs for.
		}
		return fmt.Errorf("failed to read agents.json for directory creation: %w", err)
	}

	for _, a := range agents {
		dir := config.AgentDir(worldName, a.Name, a.Role)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory for agent %q: %w", a.Name, err)
		}
	}
	return nil
}

// copyDir recursively copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy file data: %w", err)
	}
	return out.Sync()
}
