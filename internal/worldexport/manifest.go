package worldexport

import (
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// ManifestVersion is the current manifest format version.
const ManifestVersion = 1

// Manifest describes the contents and provenance of an export archive.
type Manifest struct {
	Version        int            `json:"version"`
	World          string         `json:"world"`
	ExportedAt     string         `json:"exported_at"`
	SolVersion     string         `json:"sol_version"`
	SchemaVersions SchemaVersions `json:"schema_versions"`
}

// SchemaVersions records the database schema versions at export time.
type SchemaVersions struct {
	World  int `json:"world"`
	Sphere int `json:"sphere"`
}

// Validate checks that the manifest is compatible with this binary.
// Returns an error if the archive's schema versions are newer than what
// this binary supports (forward-compatibility refusal).
func (m *Manifest) Validate() error {
	if m.Version < 1 {
		return fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if m.Version > ManifestVersion {
		return fmt.Errorf("manifest version %d is newer than supported (%d); upgrade sol first", m.Version, ManifestVersion)
	}
	if m.World == "" {
		return fmt.Errorf("manifest is missing world name")
	}
	if _, err := time.Parse(time.RFC3339, m.ExportedAt); err != nil {
		return fmt.Errorf("invalid exported_at timestamp %q: %w", m.ExportedAt, err)
	}
	if m.SchemaVersions.World > store.CurrentWorldSchema {
		return fmt.Errorf("archive world schema v%d is newer than supported v%d; upgrade sol first",
			m.SchemaVersions.World, store.CurrentWorldSchema)
	}
	if m.SchemaVersions.Sphere > store.CurrentSphereSchema {
		return fmt.Errorf("archive sphere schema v%d is newer than supported v%d; upgrade sol first",
			m.SchemaVersions.Sphere, store.CurrentSphereSchema)
	}
	return nil
}

// ExportAgent is the JSON-serializable representation of an agent record.
type ExportAgent struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	World      string `json:"world"`
	Role       string `json:"role"`
	State      string `json:"state"`
	TetherItem string `json:"tether_item,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// ExportMessage is the JSON-serializable representation of a message record.
type ExportMessage struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Subject   string `json:"subject"`
	Body      string `json:"body,omitempty"`
	Priority  int    `json:"priority"`
	Type      string `json:"type"`
	ThreadID  string `json:"thread_id,omitempty"`
	Delivery  string `json:"delivery"`
	Read      bool   `json:"read"`
	CreatedAt string `json:"created_at"`
	AckedAt   string `json:"acked_at,omitempty"`
}

// ExportEscalation is the JSON-serializable representation of an escalation record.
type ExportEscalation struct {
	ID           string `json:"id"`
	Severity     string `json:"severity"`
	Source       string `json:"source"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Acknowledged bool   `json:"acknowledged"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// ExportCaravan is the JSON-serializable representation of a caravan record.
type ExportCaravan struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Owner     string `json:"owner,omitempty"`
	CreatedAt string `json:"created_at"`
	ClosedAt  string `json:"closed_at,omitempty"`
}

// ExportCaravanItem is the JSON-serializable representation of a caravan item.
type ExportCaravanItem struct {
	CaravanID  string `json:"caravan_id"`
	WorkItemID string `json:"work_item_id"`
	World      string `json:"world"`
	Phase      int    `json:"phase"`
}
