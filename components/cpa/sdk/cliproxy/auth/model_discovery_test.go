package auth

import "testing"

func TestModelDiscoveryLifecycleAndAttemptOrdering(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a, err := m.Register(t.Context(), &Auth{ID: "discovery-a", Provider: "cpa-bps", Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	oldAttempt, nextAttempt := m.NextModelDiscoveryAttempt(), m.NextModelDiscoveryAttempt()
	if !m.RecordModelDiscovery(a, nextAttempt, ModelDiscovery{Status: "succeeded", PluginID: "cpa-bps"}) {
		t.Fatal("result rejected")
	}
	if m.RecordModelDiscovery(a, oldAttempt, ModelDiscovery{Status: "failed"}) {
		t.Fatal("older attempt overwrote result")
	}
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "succeeded" || got.CheckedAt == "" {
		t.Fatalf("result: %+v", got)
	}
	if got := m.ModelDiscoveryForAuth("other"); got.Status != "unknown" {
		t.Fatalf("cross-credential result: %+v", got)
	}
	disabled, _, err := m.Modify(t.Context(), a.ID, a.RegistrationEpoch, func(current *Auth) error { current.Disabled = true; return nil })
	if err != nil || disabled == nil {
		t.Fatal(err)
	}
	if m.RecordModelDiscovery(a, m.NextModelDiscoveryAttempt(), ModelDiscovery{Status: "failed"}) {
		t.Fatal("disabled credential accepted result")
	}
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "disabled" {
		t.Fatalf("disabled: %+v", got)
	}
	_, _, err = m.Modify(t.Context(), a.ID, a.RegistrationEpoch, func(current *Auth) error {
		current.Disabled = false
		current.Status = StatusActive
		current.ProxyURL = "http://proxy.invalid"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "unknown" {
		t.Fatalf("changed input reused result: %+v", got)
	}
	m.Remove(t.Context(), a.ID)
	if len(m.modelDiscoveries) != 0 {
		t.Fatal("deleted credential retained result")
	}
}

func TestModelDiscoveryDisableEnableRejectsPendingAttempt(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a, err := m.Register(t.Context(), &Auth{ID: t.Name(), Provider: "cpa-bps", Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	attempt := m.NextModelDiscoveryAttempt()
	for _, disabled := range []bool{true, false} {
		_, _, err = m.Modify(t.Context(), a.ID, a.RegistrationEpoch, func(current *Auth) error { current.Disabled = disabled; current.Status = StatusActive; return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	if m.RecordModelDiscovery(a, attempt, ModelDiscovery{Status: "failed"}) {
		t.Fatal("pre-disable attempt survived a toggle cycle")
	}
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "unknown" {
		t.Fatalf("stale state: %+v", got)
	}
	current, _ := m.GetByID(a.ID)
	if !m.RecordModelDiscovery(current, m.NextModelDiscoveryAttempt(), ModelDiscovery{Status: "succeeded"}) {
		t.Fatal("new attempt rejected")
	}
}
