package store

import (
	"sync"
	"testing"
)

// TestSetWritMetadataConcurrentKeys verifies that concurrent callers writing
// distinct keys both have their keys preserved (no silent drops due to a
// read-modify-write race).
func TestSetWritMetadataConcurrentKeys(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)
	writID, err := s.CreateWrit("concurrent-meta", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Each goroutine writes a distinct key.
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := "key" + string(rune('a'+i))
			err := s.SetWritMetadata(writID, map[string]any{key: i})
			if err != nil {
				t.Errorf("SetWritMetadata goroutine %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	// All keys must be present.
	got, err := s.GetWritMetadata(writID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < goroutines; i++ {
		key := "key" + string(rune('a'+i))
		if _, ok := got[key]; !ok {
			t.Errorf("key %q missing from metadata after concurrent writes; got %v", key, got)
		}
	}
}

// TestCloseWritAtomicSupersede verifies that CloseWrit atomically closes the
// writ and supersedes failed MRs — no failed MRs remain after a successful
// close.
func TestCloseWritAtomicSupersede(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)
	writID, err := s.CreateWrit("atomic-close", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create and fail two MRs.
	mr1ID, _ := s.CreateMergeRequest(writID, "branch1", 2)
	mr2ID, _ := s.CreateMergeRequest(writID, "branch2", 2)
	s.ClaimMergeRequest("forge/Forge")
	s.UpdateMergeRequestPhase(mr1ID, MRFailed)
	s.ClaimMergeRequest("forge/Forge")
	s.UpdateMergeRequestPhase(mr2ID, MRFailed)

	// Close the writ.
	superseded, err := s.CloseWrit(writID)
	if err != nil {
		t.Fatal(err)
	}

	// Both failed MRs must be reported as superseded.
	if len(superseded) != 2 {
		t.Fatalf("expected 2 superseded MR IDs, got %d: %v", len(superseded), superseded)
	}

	// Verify writ is actually closed.
	writ, err := s.GetWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if writ.Status != "closed" {
		t.Errorf("expected writ status 'closed', got %q", writ.Status)
	}

	// Verify MRs are superseded.
	mr1, _ := s.GetMergeRequest(mr1ID)
	if mr1.Phase != MRSuperseded {
		t.Errorf("expected mr1 phase 'superseded', got %q", mr1.Phase)
	}
	mr2, _ := s.GetMergeRequest(mr2ID)
	if mr2.Phase != MRSuperseded {
		t.Errorf("expected mr2 phase 'superseded', got %q", mr2.Phase)
	}
}
