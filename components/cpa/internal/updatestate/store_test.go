package updatestate

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSiteLockAndAbandonedOwnerBlockAnotherWriter(t *testing.T) {
	root := t.TempDir()
	a, err := New(filepath.Join(root, "site.lock"), filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(a.LockPath, a.Directory)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := a.Begin(Operation{ID: "plugin-first", Type: "plugin", TargetVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Begin(Operation{ID: "core-second"}); err == nil {
		t.Fatal("another writer acquired the live lock")
	}
	if err = lease.Detach("restart interrupted the operation"); err != nil {
		t.Fatal(err)
	}
	_, err = b.Begin(Operation{ID: "core-second"})
	var busy *BusyError
	if !errors.As(err, &busy) || busy.OperationID != "plugin-first" {
		t.Fatalf("abandoned operation was not preserved: %v", err)
	}
	op, err := b.Get("plugin-first")
	if err != nil || op.Status != "recovery_required" {
		t.Fatalf("wrong persisted outcome: %+v %v", op, err)
	}
}

func TestTerminalPublicationReleasesSiteMaintenance(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "site.lock"), filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Begin(Operation{ID: "plugin-first", Type: "plugin"})
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.Update(func(op *Operation) { op.Status = "reloading" }); err != nil {
		t.Fatal(err)
	}
	if err = lease.Finish(func(op *Operation) { op.Status = "completed_disabled" }); err != nil {
		t.Fatal(err)
	}
	op, err := store.Get("plugin-first")
	if err != nil || op.FinishedAt == "" || op.Status != "completed_disabled" {
		t.Fatalf("wrong persisted terminal: %+v %v", op, err)
	}
	next, err := store.Begin(Operation{ID: "core-second"})
	if err != nil {
		t.Fatal(err)
	}
	if err = next.Finish(func(op *Operation) { op.Status = "failed" }); err != nil {
		t.Fatal(err)
	}
}

func TestOperationActionRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "site.lock"), filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Begin(Operation{ID: "action-test", Type: "plugin", PluginID: "cpa-bps", Action: "configure"})
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.Finish(func(op *Operation) { op.Status = "failed"; op.Stage = "failed" }); err != nil {
		t.Fatal(err)
	}
	op, err := store.Get("action-test")
	if err != nil || op.Action != "configure" || op.PluginID != "cpa-bps" {
		t.Fatalf("roundtrip: %+v %v", op, err)
	}
}
