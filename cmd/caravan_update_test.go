package cmd

import (
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// TestCaravanUpdateNotifyToggle covers the new `sol caravan update --notify`
// command (sol-e6836759ad1321fb): a caravan created without --notify can be
// toggled on and off post-create via `caravan update`.
func TestCaravanUpdateNotifyToggle(t *testing.T) {
	world := "caravanupdatetest"
	caravanID := setupCaravanWorldTest(t, world)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	c, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if c.NotifyOnClose {
		t.Fatal("expected NotifyOnClose to default to false")
	}

	if err := runCaravanCmd(t, "caravan", "update", caravanID, "--notify=on"); err != nil {
		t.Fatalf("caravan update --notify=on: %v", err)
	}
	c, err = sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.NotifyOnClose {
		t.Fatal("expected NotifyOnClose to be true after 'caravan update --notify=on'")
	}

	if err := runCaravanCmd(t, "caravan", "update", caravanID, "--notify=off"); err != nil {
		t.Fatalf("caravan update --notify=off: %v", err)
	}
	c, err = sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if c.NotifyOnClose {
		t.Fatal("expected NotifyOnClose to be false after 'caravan update --notify=off'")
	}
}

// TestCaravanUpdateNotifyAcceptsTrueFalseSynonyms verifies --notify accepts
// true/false in addition to on/off, as documented in the command's help.
func TestCaravanUpdateNotifyAcceptsTrueFalseSynonyms(t *testing.T) {
	world := "caravanupdatetruefalse"
	caravanID := setupCaravanWorldTest(t, world)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if err := runCaravanCmd(t, "caravan", "update", caravanID, "--notify=true"); err != nil {
		t.Fatalf("caravan update --notify=true: %v", err)
	}
	c, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.NotifyOnClose {
		t.Fatal("expected NotifyOnClose to be true after 'caravan update --notify=true'")
	}

	if err := runCaravanCmd(t, "caravan", "update", caravanID, "--notify=false"); err != nil {
		t.Fatalf("caravan update --notify=false: %v", err)
	}
	c, err = sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if c.NotifyOnClose {
		t.Fatal("expected NotifyOnClose to be false after 'caravan update --notify=false'")
	}
}

// TestCaravanUpdateUnknownIDErrorsCleanly verifies an unknown caravan ID
// produces a clean error rather than a panic or opaque failure.
func TestCaravanUpdateUnknownIDErrorsCleanly(t *testing.T) {
	world := "caravanupdateunknown"
	setupCaravanWorldTest(t, world)

	err := runCaravanCmd(t, "caravan", "update", "car-doesnotexist0", "--notify=on")
	if err == nil {
		t.Fatal("expected error for unknown caravan id")
	}
	if !strings.Contains(err.Error(), "car-doesnotexist0") {
		t.Errorf("expected error to mention the unknown id, got: %v", err)
	}
}

// TestCaravanUpdateRequiresNotifyFlag verifies the command rejects an
// invocation with no --notify flag rather than silently no-op'ing.
func TestCaravanUpdateRequiresNotifyFlag(t *testing.T) {
	world := "caravanupdatenoflag"
	caravanID := setupCaravanWorldTest(t, world)

	err := runCaravanCmd(t, "caravan", "update", caravanID)
	if err == nil {
		t.Fatal("expected error when no --notify flag is passed")
	}
}

// TestCaravanUpdateRejectsInvalidNotifyValue verifies an unrecognized
// --notify value is rejected with a helpful error.
func TestCaravanUpdateRejectsInvalidNotifyValue(t *testing.T) {
	world := "caravanupdatebadvalue"
	caravanID := setupCaravanWorldTest(t, world)

	err := runCaravanCmd(t, "caravan", "update", caravanID, "--notify=maybe")
	if err == nil {
		t.Fatal("expected error for invalid --notify value")
	}
	if !strings.Contains(err.Error(), "on/off") {
		t.Errorf("expected error to hint at accepted values, got: %v", err)
	}
}
