package store

import (
	"encoding/json"
	"fmt"
)

// Protocol message subject prefixes.
const (
	ProtoPolecatDone    = "POLECAT_DONE"
	ProtoMergeReady     = "MERGE_READY"
	ProtoMerged         = "MERGED"
	ProtoMergeFailed    = "MERGE_FAILED"
	ProtoReworkRequest  = "REWORK_REQUEST"
	ProtoRecoveryNeeded = "RECOVERY_NEEDED"
)

// PolecatDonePayload is sent when a polecat completes its work.
type PolecatDonePayload struct {
	WorkItemID string `json:"work_item_id"`
	AgentID    string `json:"agent_id"`
	Branch     string `json:"branch"`
	Rig        string `json:"rig"`
}

// MergeReadyPayload is sent when a witness verifies polecat work.
type MergeReadyPayload struct {
	MergeRequestID string `json:"merge_request_id"`
	WorkItemID     string `json:"work_item_id"`
	Branch         string `json:"branch"`
}

// MergedPayload is sent when the refinery successfully merges work.
type MergedPayload struct {
	MergeRequestID string `json:"merge_request_id"`
	WorkItemID     string `json:"work_item_id"`
}

// MergeFailedPayload is sent when a merge fails (conflict or gate failure).
type MergeFailedPayload struct {
	MergeRequestID string `json:"merge_request_id"`
	WorkItemID     string `json:"work_item_id"`
	Reason         string `json:"reason"`
}

// RecoveryNeededPayload is sent when a witness detects a polecat issue.
type RecoveryNeededPayload struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
	Reason     string `json:"reason"`
	Attempts   int    `json:"attempts"`
}

// SendProtocolMessage sends a typed protocol message with a JSON body.
// The subject is the protocol type (e.g., "POLECAT_DONE").
// The body is JSON-encoded from the payload.
// Protocol messages always use priority=1 (urgent).
func (s *Store) SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal protocol payload: %w", err)
	}
	return s.SendMessage(sender, recipient, protoType, string(body), 1, "protocol")
}

// PendingProtocol returns pending protocol messages for a recipient,
// filtered by protocol type. If protoType is empty, returns all protocol messages.
func (s *Store) PendingProtocol(recipient, protoType string) ([]Message, error) {
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at
	          FROM messages WHERE delivery = 'pending' AND type = 'protocol' AND recipient = ?`
	args := []interface{}{recipient}

	if protoType != "" {
		query += ` AND subject = ?`
		args = append(args, protoType)
	}
	query += ` ORDER BY priority ASC, created_at ASC`

	return s.scanMessages(query, args...)
}
