package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
)

func ae1Observe(t *testing.T, m *Manager, id string, used string, week bool, ticketOverride *kiloPolicyTicket) {
	t.Helper()
	a, _ := m.GetByID(id)
	p := kiloSourcePolicy(a)
	now := time.Now().UTC()
	body := fmt.Sprintf(`{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":%s,"reset_at":%d}%s}}`, used, now.Add(time.Hour).Unix(), "")
	if week {
		body = fmt.Sprintf(`{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":%s,"reset_at":%d},"secondary_window":{"limit_window_seconds":604800,"used_percent":1,"reset_at":%d}}}`, used, now.Add(time.Hour).Unix(), now.Add(24*time.Hour).Unix())
	}
	obs, err := usage.ParseUsage([]byte(body), now)
	if err != nil {
		t.Fatal(err)
	}
	ticket := kiloPolicyTicket{revision: p.Revision, pause: p.PauseGeneration, identity: kiloIdentityRevision(a), epoch: a.RegistrationEpoch, binding: kiloQuotaBinding(a).Revision, started: now}
	if ticketOverride != nil {
		ticket = *ticketOverride
	}
	m.observeKiloSourcePolicy(context.Background(), id, ticket, KiloQuotaView{BindingRevision: ticket.binding, Query: KiloQuotaQueryView{Status: "success", ObservedAt: &now}, UsageObservation: obs})
}
func TestAE1SourcePolicyPauseScopeAndDisableIsolation(t *testing.T) {
	m, a := m6Manager(t)
	ctx := context.Background()
	if _, err := m.ChangeKiloSourcePolicy(ctx, a.ID, 0, true, 1800, false); err != nil {
		t.Fatal(err)
	}
	ae1Observe(t, m, a.ID, "99.999", true, nil)
	v, _ := m.KiloSourcePolicy(a.ID)
	if v.Paused || v.Scope != "18000+604800" {
		t.Fatalf("rounded percentage: %#v", v)
	}
	before, _ := m.GetByID(a.ID)
	old := kiloPolicyTicket{revision: v.Revision, pause: v.PauseGeneration, identity: kiloIdentityRevision(before), epoch: before.RegistrationEpoch, started: time.Now()}
	ae1Observe(t, m, a.ID, "100", true, nil)
	live, _ := m.GetByID(a.ID)
	if !kiloAutoBlocked(live) {
		t.Fatal("actual selection did not block")
	}
	ae1Observe(t, m, a.ID, "1", true, &old)
	v, _ = m.KiloSourcePolicy(a.ID)
	if !v.Paused {
		t.Fatal("old result cleared pause")
	}
	ae1Observe(t, m, a.ID, "1", false, nil)
	v, _ = m.KiloSourcePolicy(a.ID)
	if !v.Paused || v.Status != "source_pending" {
		t.Fatal("missing window cleared pause")
	}
	m.MarkResult(ctx, m6Result(live, "subscription_sharing_usage_limit_exceeded", 200, 0))
	if _, err := m.ChangeKiloSourcePolicy(ctx, a.ID, v.Revision, false, 1800, false); err != nil {
		t.Fatal(err)
	}
	live, _ = m.GetByID(a.ID)
	if kiloSourcePolicy(live).Paused || kiloRestriction(live).Reason != kiloQuotaExhausted || !kiloAutoBlocked(live) {
		t.Fatal("disable cleared unrelated restriction")
	}
}
func TestAE1SourcePolicyRebindKeepsPause(t *testing.T) {
	m, a := m6Manager(t)
	ctx := context.Background()
	if _, err := m.ChangeKiloSourcePolicy(ctx, a.ID, 0, true, 1800, false); err != nil {
		t.Fatal(err)
	}
	ae1Observe(t, m, a.ID, "100", true, nil)
	rev := uint64(0)
	if _, err := m.ChangeKiloAccount(ctx, a.ID, KiloAccountChanges{QuotaSource: &KiloQuotaBinding{Kind: "imported", AccessToken: "synthetic"}, ExpectedBindingRevision: &rev}, false, false); err != nil {
		t.Fatal(err)
	}
	v, _ := m.KiloSourcePolicy(a.ID)
	if !v.Paused || v.Status != "source_pending" || v.Scope != "" {
		t.Fatalf("rebind: %#v", v)
	}
	ae1Observe(t, m, a.ID, "1", true, nil)
	v, _ = m.KiloSourcePolicy(a.ID)
	if v.Paused {
		t.Fatal("fresh complete source did not recover")
	}
}
