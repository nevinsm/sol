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
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// ExportOptions controls export behavior.
type ExportOptions struct {
	World      string // world to export
	OutputPath string // path for output archive (empty = "<world>-export.tar.gz")
	SolVersion string // sol binary version for the manifest (empty = "dev")
}

// ExportResult describes the outcome of an export.
type ExportResult struct {
	OutputPath string // path to the created archive
	Size       int64  // size in bytes
}

// Export creates a world export archive that is compatible with Import.
//
// The archive layout matches what Import() expects:
//
//	sol-export-{world}-{timestamp}/
//	  manifest.json     (Version=1, World, ExportedAt, SolVersion, SchemaVersions)
//	  world.db          (WAL-checkpointed world database)
//	  world.toml        (world configuration)
//	  sphere-data/
//	    agents.json
//	    messages.json
//	    escalations.json
//	    caravans.json
//	    caravan_items.json
//	    caravan_dependencies.json
//	  writ-outputs/          (if present)
//	    {writID}/
//	      ...
func Export(opts ExportOptions) (*ExportResult, error) {
	world := opts.World

	outputPath := opts.OutputPath
	if outputPath == "" {
		outputPath = world + "-export.tar.gz"
	}

	solVersion := opts.SolVersion
	if solVersion == "" {
		solVersion = "dev"
	}

	// 1. Checkpoint world DB WAL so the .db file is self-contained.
	worldStore, err := store.OpenWorld(world)
	if err != nil {
		return nil, err
	}
	defer worldStore.Close()
	if err := worldStore.Checkpoint(); err != nil {
		return nil, fmt.Errorf("failed to checkpoint world database: %w", err)
	}

	// 2. Open sphere store and gather world-scoped sphere data.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return nil, fmt.Errorf("failed to open sphere database: %w", err)
	}
	defer sphereStore.Close()

	agents, err := sphereStore.ListAgents(world, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for export: %w", err)
	}

	messages, err := sphereStore.ExportMessagesForWorld(world)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages for export: %w", err)
	}

	escalations, err := sphereStore.ExportEscalationsForWorld(world)
	if err != nil {
		return nil, fmt.Errorf("failed to list escalations for export: %w", err)
	}

	caravans, err := sphereStore.ExportCaravansForWorld(world)
	if err != nil {
		return nil, fmt.Errorf("failed to list caravans for export: %w", err)
	}

	caravanItems, err := sphereStore.ExportCaravanItemsForWorld(world)
	if err != nil {
		return nil, fmt.Errorf("failed to list caravan items for export: %w", err)
	}

	caravanDeps, err := sphereStore.ExportCaravanDependenciesForWorld(world)
	if err != nil {
		return nil, fmt.Errorf("failed to list caravan dependencies for export: %w", err)
	}

	// 3. Create output archive file.
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file %q: %w", outputPath, err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	now := time.Now().UTC()
	prefix := "sol-export-" + world + "-" + now.Format("20060102T150405") + "/"

	// Write top-level directory entry.
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     prefix,
		Mode:     0o755,
		ModTime:  now,
	}); err != nil {
		return nil, fmt.Errorf("failed to write archive root: %w", err)
	}

	// 4. Write manifest.json.
	manifest := Manifest{
		Version:    ManifestVersion,
		World:      world,
		ExportedAt: now.Format(time.RFC3339),
		SolVersion: solVersion,
		SchemaVersions: SchemaVersions{
			World:  store.CurrentWorldSchema,
			Sphere: store.CurrentSphereSchema,
		},
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := exportWriteBytes(tw, manifestJSON, prefix+"manifest.json", now); err != nil {
		return nil, fmt.Errorf("failed to add manifest: %w", err)
	}

	// 5. Write world.db.
	dbPath := filepath.Join(config.StoreDir(), world+".db")
	if err := exportWriteFile(tw, dbPath, prefix+"world.db"); err != nil {
		return nil, fmt.Errorf("failed to add world database: %w", err)
	}

	// 6. Write world.toml.
	tomlPath := config.WorldConfigPath(world)
	if err := exportWriteFile(tw, tomlPath, prefix+"world.toml"); err != nil {
		return nil, fmt.Errorf("failed to add world config: %w", err)
	}

	// 7. Write sphere-data/ directory.
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     prefix + "sphere-data/",
		Mode:     0o755,
		ModTime:  now,
	}); err != nil {
		return nil, fmt.Errorf("failed to write sphere-data directory: %w", err)
	}

	// Agents.
	// Runtime fields (state, active_writ) are intentionally omitted — see
	// ExportAgent's doc comment.
	exportAgents := make([]ExportAgent, len(agents))
	for i, a := range agents {
		exportAgents[i] = ExportAgent{
			ID:        a.ID,
			Name:      a.Name,
			World:     a.World,
			Role:      a.Role,
			CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: a.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	if err := exportWriteJSON(tw, exportAgents, prefix+"sphere-data/agents.json", now); err != nil {
		return nil, fmt.Errorf("failed to add agents: %w", err)
	}

	// Messages.
	exportMessages := make([]ExportMessage, len(messages))
	for i, m := range messages {
		em := ExportMessage{
			ID:        m.ID,
			Sender:    m.Sender,
			Recipient: m.Recipient,
			Subject:   m.Subject,
			Body:      m.Body,
			Priority:  m.Priority,
			Type:      m.Type,
			ThreadID:  m.ThreadID,
			Delivery:  m.Delivery,
			Read:      m.Read,
			CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		}
		if m.AckedAt != nil {
			em.AckedAt = m.AckedAt.UTC().Format(time.RFC3339)
		}
		exportMessages[i] = em
	}
	if err := exportWriteJSON(tw, exportMessages, prefix+"sphere-data/messages.json", now); err != nil {
		return nil, fmt.Errorf("failed to add messages: %w", err)
	}

	// Escalations.
	exportEscalations := make([]ExportEscalation, len(escalations))
	for i, e := range escalations {
		ee := ExportEscalation{
			ID:           e.ID,
			Severity:     e.Severity,
			Source:       e.Source,
			Description:  e.Description,
			SourceRef:    e.SourceRef,
			Status:       e.Status,
			Acknowledged: e.Acknowledged,
			CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:    e.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if e.LastNotifiedAt != nil {
			ee.LastNotifiedAt = e.LastNotifiedAt.UTC().Format(time.RFC3339)
		}
		exportEscalations[i] = ee
	}
	if err := exportWriteJSON(tw, exportEscalations, prefix+"sphere-data/escalations.json", now); err != nil {
		return nil, fmt.Errorf("failed to add escalations: %w", err)
	}

	// Caravans.
	exportCaravans := make([]ExportCaravan, len(caravans))
	for i, c := range caravans {
		ec := ExportCaravan{
			ID:        c.ID,
			Name:      c.Name,
			Status:    c.Status,
			Owner:     c.Owner,
			CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		}
		if c.ClosedAt != nil {
			ec.ClosedAt = c.ClosedAt.UTC().Format(time.RFC3339)
		}
		exportCaravans[i] = ec
	}
	if err := exportWriteJSON(tw, exportCaravans, prefix+"sphere-data/caravans.json", now); err != nil {
		return nil, fmt.Errorf("failed to add caravans: %w", err)
	}

	// Caravan items.
	exportItems := make([]ExportCaravanItem, len(caravanItems))
	for i, ci := range caravanItems {
		exportItems[i] = ExportCaravanItem{
			CaravanID: ci.CaravanID,
			WritID:    ci.WritID,
			World:     ci.World,
			Phase:     ci.Phase,
		}
	}
	if err := exportWriteJSON(tw, exportItems, prefix+"sphere-data/caravan_items.json", now); err != nil {
		return nil, fmt.Errorf("failed to add caravan items: %w", err)
	}

	// Caravan dependencies.
	exportDeps := make([]ExportCaravanDependency, len(caravanDeps))
	for i, d := range caravanDeps {
		exportDeps[i] = ExportCaravanDependency{
			FromID: d.FromID,
			ToID:   d.ToID,
		}
	}
	if err := exportWriteJSON(tw, exportDeps, prefix+"sphere-data/caravan_dependencies.json", now); err != nil {
		return nil, fmt.Errorf("failed to add caravan dependencies: %w", err)
	}

	// 8. Write writ-outputs/ directory (if it exists).
	writOutputsDir := filepath.Join(config.WorldDir(world), "writ-outputs")
	if info, err := os.Stat(writOutputsDir); err == nil && info.IsDir() {
		if err := exportWriteDir(tw, writOutputsDir, prefix+"writ-outputs/"); err != nil {
			return nil, fmt.Errorf("failed to add writ-outputs: %w", err)
		}
	}

	// Flush writers in order.
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize gzip: %w", err)
	}

	stat, err := os.Stat(outputPath)
	var size int64
	if err == nil {
		size = stat.Size()
	}
	return &ExportResult{OutputPath: outputPath, Size: size}, nil
}

// exportWriteFile adds a regular file from the filesystem to the tar archive.
func exportWriteFile(tw *tar.Writer, srcPath, archiveName string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	header := &tar.Header{
		Name:    archiveName,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}

// exportWriteBytes adds in-memory content as a file to the tar archive.
func exportWriteBytes(tw *tar.Writer, data []byte, archiveName string, modTime time.Time) error {
	header := &tar.Header{
		Name:    archiveName,
		Size:    int64(len(data)),
		Mode:    0o644,
		ModTime: modTime,
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// exportWriteDir recursively adds a directory tree to the tar archive.
// archivePrefix must end with "/".
func exportWriteDir(tw *tar.Writer, srcDir, archivePrefix string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		archiveName := archivePrefix + filepath.ToSlash(rel)
		if info.IsDir() {
			if !strings.HasSuffix(archiveName, "/") {
				archiveName += "/"
			}
			return tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     archiveName,
				Mode:     0o755,
				ModTime:  info.ModTime(),
			})
		}
		return exportWriteFile(tw, path, archiveName)
	})
}

// exportWriteJSON marshals v as indented JSON and writes it to the tar archive.
func exportWriteJSON(tw *tar.Writer, v interface{}, archiveName string, modTime time.Time) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", archiveName, err)
	}
	return exportWriteBytes(tw, data, archiveName, modTime)
}
