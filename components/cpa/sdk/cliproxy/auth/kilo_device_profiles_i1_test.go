package auth

import (
	"context"
	"math"
	"testing"
)

func TestI1ProfileStoreBusyUsesExistingGates(t *testing.T) {
	m := NewManager(nil, nil, nil)
	if !m.authLoadGate.TryAcquire(math.MaxInt64) {
		t.Fatal("load gate fixture")
	}
	_, err := m.tryPersistKiloDeviceProfiles(context.Background(), "i1-account", 1)
	if !isRequestScopedError(err) || err == nil {
		t.Fatalf("load contention=%v", err)
	}
	m.authLoadGate.Release(math.MaxInt64)
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	m.authMutationLocks.Store("i1-account", gate)
	_, err = m.tryPersistKiloDeviceProfiles(context.Background(), "i1-account", 1)
	if !isRequestScopedError(err) || err == nil {
		t.Fatalf("mutation contention=%v", err)
	}
	if !m.authLoadGate.TryAcquire(math.MaxInt64) {
		t.Fatal("busy mutation retained load reservation")
	}
	m.authLoadGate.Release(math.MaxInt64)
	<-gate
}
