// Package worlds provides the CLI API type for world entities.
package worlds

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// World is the CLI API representation of a world.
type World struct {
	Name           string    `json:"name"`
	SourceRepo     string    `json:"source_repo"`
	Branch         string    `json:"branch"`
	State          string    `json:"state"`
	Health         string    `json:"health"`
	AgentsCount    int       `json:"agents_count"`
	QueueCount     int       `json:"queue_count"`
	Sleeping       bool      `json:"sleeping"`
	DefaultAccount string    `json:"default_account,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// WorldInfo holds computed runtime state passed alongside the store.World record.
type WorldInfo struct {
	Branch         string
	State          string
	Health         string
	AgentsCount    int
	QueueCount     int
	Sleeping       bool
	DefaultAccount string
}

// FromStoreWorld converts a store.World plus runtime WorldInfo to the CLI API World type.
func FromStoreWorld(w store.World, info WorldInfo) World {
	return World{
		Name:           w.Name,
		SourceRepo:     w.SourceRepo,
		Branch:         info.Branch,
		State:          info.State,
		Health:         info.Health,
		AgentsCount:    info.AgentsCount,
		QueueCount:     info.QueueCount,
		Sleeping:       info.Sleeping,
		DefaultAccount: info.DefaultAccount,
		CreatedAt:      w.CreatedAt,
	}
}
