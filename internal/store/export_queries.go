package store

import (
	"database/sql"
	"fmt"
	"unicode/utf8"
)

// ExportMessagesForWorld returns all messages where the sender or recipient
// belongs to the given world (ID prefix "world/").
// Used during world export to capture world-scoped message history.
func (s *SphereStore) ExportMessagesForWorld(world string) ([]Message, error) {
	// Use exact prefix matching (substr) instead of LIKE to avoid
	// case-insensitive matches and wildcard characters (%, _) in world names.
	prefix := world + "/"
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at
	          FROM messages
	          WHERE (length(sender) > ? AND substr(sender, 1, ?) = ?)
	             OR (length(recipient) > ? AND substr(recipient, 1, ?) = ?)
	          ORDER BY created_at ASC`
	prefixLen := utf8.RuneCountInString(prefix)
	return s.scanMessages(query, prefixLen, prefixLen, prefix, prefixLen, prefixLen, prefix)
}

// ExportEscalationsForWorld returns all escalations where the source belongs to
// the given world (ID prefix "world/").
// Used during world export to capture world-scoped escalation history.
func (s *SphereStore) ExportEscalationsForWorld(world string) ([]Escalation, error) {
	// Use exact prefix matching (substr) instead of LIKE to avoid
	// case-insensitive matches and wildcard characters (%, _) in world names.
	prefix := world + "/"
	query := `SELECT id, severity, source, description, source_ref, status, acknowledged, last_notified_at, created_at, updated_at
	          FROM escalations
	          WHERE length(source) > ? AND substr(source, 1, ?) = ?
	          ORDER BY created_at ASC`
	prefixLen := utf8.RuneCountInString(prefix)
	return s.scanEscalations(query, prefixLen, prefixLen, prefix)
}

// ExportCaravanItemsForWorld returns all caravan items belonging to the given world.
// Used during world export.
func (s *SphereStore) ExportCaravanItemsForWorld(world string) ([]CaravanItem, error) {
	rows, err := s.db.Query(
		`SELECT caravan_id, writ_id, world, phase FROM caravan_items WHERE world = ? ORDER BY caravan_id, phase, writ_id`,
		world,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to export caravan items for world %q: %w", world, err)
	}
	defer rows.Close()

	var items []CaravanItem
	for rows.Next() {
		var ci CaravanItem
		if err := rows.Scan(&ci.CaravanID, &ci.WritID, &ci.World, &ci.Phase); err != nil {
			return nil, fmt.Errorf("failed to scan caravan item: %w", err)
		}
		items = append(items, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravan items: %w", err)
	}
	return items, nil
}

// ExportCaravansForWorld returns all caravans that have at least one item belonging
// to the given world. Used during world export to preserve caravan context.
func (s *SphereStore) ExportCaravansForWorld(world string) ([]Caravan, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT c.id, c.name, c.status, c.owner, c.created_at, c.closed_at
		 FROM caravans c
		 JOIN caravan_items ci ON c.id = ci.caravan_id
		 WHERE ci.world = ?
		 ORDER BY c.created_at DESC`,
		world,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to export caravans for world %q: %w", world, err)
	}
	defer rows.Close()

	var caravans []Caravan
	for rows.Next() {
		var c Caravan
		var owner sql.NullString
		var closedAt sql.NullString
		var createdAt string

		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &owner, &createdAt, &closedAt); err != nil {
			return nil, fmt.Errorf("failed to scan caravan: %w", err)
		}
		c.Owner = owner.String
		var parseErr error
		if c.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "caravan "+c.ID); parseErr != nil {
			return nil, parseErr
		}
		if c.ClosedAt, parseErr = parseOptionalRFC3339(closedAt, "closed_at", "caravan "+c.ID); parseErr != nil {
			return nil, parseErr
		}
		caravans = append(caravans, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravans: %w", err)
	}
	return caravans, nil
}

// CaravanDependency represents a dependency relationship between two caravans.
type CaravanDependency struct {
	FromID string
	ToID   string
}

// ExportCaravanDependenciesForWorld returns all caravan dependency relationships
// where the dependent (from_id) caravan has at least one item in the given world.
// Used during world export to preserve caravan sequencing guarantees.
func (s *SphereStore) ExportCaravanDependenciesForWorld(world string) ([]CaravanDependency, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT cd.from_id, cd.to_id
		 FROM caravan_dependencies cd
		 JOIN caravan_items ci ON cd.from_id = ci.caravan_id
		 WHERE ci.world = ?
		 ORDER BY cd.from_id, cd.to_id`,
		world,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to export caravan dependencies for world %q: %w", world, err)
	}
	defer rows.Close()

	var deps []CaravanDependency
	for rows.Next() {
		var d CaravanDependency
		if err := rows.Scan(&d.FromID, &d.ToID); err != nil {
			return nil, fmt.Errorf("failed to scan caravan dependency: %w", err)
		}
		deps = append(deps, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravan dependencies: %w", err)
	}
	return deps, nil
}
