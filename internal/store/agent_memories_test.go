package store

import (
	"testing"
	"time"
)

func TestSetAgentMemoryPreservesCreatedAt(t *testing.T) {
	s := setupWorld(t)

	// Set initial memory.
	if err := s.SetAgentMemory("Toast", "preference", "dark-mode"); err != nil {
		t.Fatal(err)
	}

	// Get the initial created_at.
	memories, err := s.ListAgentMemories("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	originalCreatedAt := memories[0].CreatedAt
	if memories[0].Value != "dark-mode" {
		t.Fatalf("expected value 'dark-mode', got %q", memories[0].Value)
	}

	// Wait a moment to ensure timestamps differ.
	time.Sleep(10 * time.Millisecond)

	// Update the same key — should preserve created_at.
	if err := s.SetAgentMemory("Toast", "preference", "light-mode"); err != nil {
		t.Fatal(err)
	}

	memories, err = s.ListAgentMemories("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory after update, got %d", len(memories))
	}
	if memories[0].Value != "light-mode" {
		t.Fatalf("expected value 'light-mode', got %q", memories[0].Value)
	}
	if !memories[0].CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("created_at changed on update: original=%v, after=%v",
			originalCreatedAt, memories[0].CreatedAt)
	}
}

func TestSetAgentMemoryCRUD(t *testing.T) {
	s := setupWorld(t)

	// Set memory.
	if err := s.SetAgentMemory("Toast", "key1", "val1"); err != nil {
		t.Fatal(err)
	}

	// List memories.
	memories, err := s.ListAgentMemories("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].AgentName != "Toast" {
		t.Fatalf("expected agent_name 'Toast', got %q", memories[0].AgentName)
	}
	if memories[0].Key != "key1" {
		t.Fatalf("expected key 'key1', got %q", memories[0].Key)
	}
	if memories[0].Value != "val1" {
		t.Fatalf("expected value 'val1', got %q", memories[0].Value)
	}

	// Delete memory.
	if err := s.DeleteAgentMemory("Toast", "key1"); err != nil {
		t.Fatal(err)
	}
	memories, err = s.ListAgentMemories("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 0 {
		t.Fatalf("expected 0 memories after delete, got %d", len(memories))
	}
}

func TestDeleteAgentMemoryNotFound(t *testing.T) {
	s := setupWorld(t)

	err := s.DeleteAgentMemory("Toast", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}
