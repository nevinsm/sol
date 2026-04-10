// Package caravans provides the CLI API types for caravan entities.
package caravans

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// Caravan is the CLI API representation of a caravan.
type Caravan struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Status        string        `json:"status"`
	Owner         string        `json:"owner"`
	Worlds        []string      `json:"worlds"`
	ItemsTotal    int           `json:"items_total"`
	ItemsMerged   int           `json:"items_merged"`
	ItemsInProg   int           `json:"items_in_progress"`
	ItemsReady    int           `json:"items_ready"`
	ItemsBlocked  int           `json:"items_blocked"`
	PhaseProgress []PhaseCount  `json:"phase_progress"`
	CreatedAt     time.Time     `json:"created_at"`
	ClosedAt      *time.Time    `json:"closed_at,omitempty"`
}

// PhaseCount tracks per-phase item counts.
type PhaseCount struct {
	Phase  int `json:"phase"`
	Total  int `json:"total"`
	Merged int `json:"merged"`
}

// CaravanItem is the CLI API representation of a caravan item.
type CaravanItem struct {
	WritID   string `json:"writ_id"`
	World    string `json:"world"`
	Phase    int    `json:"phase"`
	Status   string `json:"status"`
	Ready    bool   `json:"ready"`
	Assignee string `json:"assignee,omitempty"`
}

// FromStoreCaravan converts a store.Caravan and its item statuses to the CLI API Caravan type.
func FromStoreCaravan(c store.Caravan, items []store.CaravanItemStatus) Caravan {
	worlds := map[string]bool{}
	phases := map[int]*PhaseCount{}
	var merged, inProg, ready, blocked int

	for _, item := range items {
		worlds[item.World] = true

		pc, ok := phases[item.Phase]
		if !ok {
			pc = &PhaseCount{Phase: item.Phase}
			phases[item.Phase] = pc
		}
		pc.Total++

		switch {
		case item.WritStatus == "merged" || item.WritStatus == "closed":
			merged++
			pc.Merged++
		case item.IsDispatched():
			inProg++
		case item.Ready:
			ready++
		default:
			blocked++
		}
	}

	worldList := make([]string, 0, len(worlds))
	for w := range worlds {
		worldList = append(worldList, w)
	}

	phaseList := make([]PhaseCount, 0, len(phases))
	for i := 0; i <= maxPhase(phases); i++ {
		if pc, ok := phases[i]; ok {
			phaseList = append(phaseList, *pc)
		}
	}
	if phaseList == nil {
		phaseList = []PhaseCount{}
	}

	return Caravan{
		ID:            c.ID,
		Name:          c.Name,
		Status:        c.Status,
		Owner:         c.Owner,
		Worlds:        worldList,
		ItemsTotal:    len(items),
		ItemsMerged:   merged,
		ItemsInProg:   inProg,
		ItemsReady:    ready,
		ItemsBlocked:  blocked,
		PhaseProgress: phaseList,
		CreatedAt:     c.CreatedAt,
		ClosedAt:      c.ClosedAt,
	}
}

// FromStoreCaravanItem converts a store.CaravanItemStatus to the CLI API CaravanItem type.
func FromStoreCaravanItem(item store.CaravanItemStatus) CaravanItem {
	return CaravanItem{
		WritID:   item.WritID,
		World:    item.World,
		Phase:    item.Phase,
		Status:   item.WritStatus,
		Ready:    item.Ready,
		Assignee: item.Assignee,
	}
}

func maxPhase(m map[int]*PhaseCount) int {
	max := -1
	for p := range m {
		if p > max {
			max = p
		}
	}
	return max
}
