package auth

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func m6Manager(t *testing.T) (*Manager, *Auth) {
	t.Helper()
	m := NewManager(&kiloMutationStore{}, nil, nil)
	m.SetConfig(&config.Config{AuthDir: filepath.Join(t.TempDir(), "auth")})
	key := "synthetic-m6-key"
	a, err := m.ChangeKiloAccount(context.Background(), "", KiloAccountChanges{APIKey: &key}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	return m, a
}
func m6Result(a *Auth, code string, status int, seq uint64) Result {
	doc := ojson.NewDocString(`{"error":{"message":"fixed","code":""}}`)
	_ = doc.Set("error.code", code)
	_ = doc.Set("upstream_status", status)
	return Result{Provider: "kilo", AuthID: a.ID, Model: "gpt-6.1-sol", ExecutionSeq: seq, RegistrationEpoch: a.RegistrationEpoch, CredentialVersion: a.CredentialVersion, Error: &Error{Code: code, Message: doc.String(), HTTPStatus: status}}
}
func TestKiloM6ManualFenceAndRestrictedPrecedence(t *testing.T) {
	m, a := m6Manager(t)
	m.executionSeq.Store(5)
	m.MarkResult(context.Background(), m6Result(a, "account_restricted", 403, 1))
	m.MarkResult(context.Background(), m6Result(a, "subscription_sharing_usage_limit_exceeded", 200, 2))
	if m.KiloRestriction(a.ID).Reason != kiloAccountRestricted {
		t.Fatal("late quota weakened restricted")
	}
	enabled := true
	if _, err := m.ChangeKiloAccount(context.Background(), a.ID, KiloAccountChanges{Enabled: &enabled}, false, false); err != nil {
		t.Fatal(err)
	}
	m.MarkResult(context.Background(), m6Result(a, "account_restricted", 403, 3))
	if m.KiloRestriction(a.ID).Reason != "none" {
		t.Fatal("late pre-manual execution re-restricted")
	}
}
func TestKiloM6ReloadProtectsUnsavedRuntimeAndIdentity(t *testing.T) {
	m, a := m6Manager(t)
	m.MarkResult(context.Background(), m6Result(a, "subscription_sharing_usage_limit_exceeded", 200, 0))
	live, _ := m.GetByID(a.ID)
	v := kiloRestriction(live)
	future := time.Now().Add(time.Hour)
	v.RecoverAt = &future
	setKiloRestriction(live, v)
	// An older snapshot may not drop the live restriction or manufacture a plan
	// for a replacement identity.
	incoming := a.Clone()
	preserveKiloRestriction(live, incoming)
	if kiloRestriction(incoming).Reason != kiloQuotaExhausted {
		t.Fatal("snapshot erased restriction")
	}
	same := live.Clone()
	oldPlan := v
	oldPlan.RecoverAt = nil
	setKiloRestriction(same, oldPlan)
	preserveKiloRestriction(live, same)
	if kiloRestriction(same).RecoverAt == nil {
		t.Fatal("same-generation snapshot erased unsaved plan")
	}
	incoming = a.Clone()
	incoming.Metadata["api_key"] = "different"
	preserveKiloRestriction(live, incoming)
	if next := kiloRestriction(incoming); next.RecoverAt != nil || next.Generation <= v.Generation {
		t.Fatal("identity inherited old reset")
	}
}

type m6ClockHook struct {
	NoopHook
	updates chan *Auth
}

func (h *m6ClockHook) OnAuthUpdated(_ context.Context, a *Auth) {
	select {
	case h.updates <- a:
	default:
	}
}
func TestKiloM6HostTickAndCancellation(t *testing.T) {
	m, a := m6Manager(t)
	now := time.Now().UTC()
	due := now.Add(time.Hour)
	v := KiloRestrictionView{Reason: kiloQuotaExhausted, Generation: 10, IdentityRevision: kiloIdentityRevision(a), SourceRevision: 1, RecoverAt: &due}
	applyKiloQuotaBinding(a, KiloQuotaBinding{Kind: "imported", Revision: 1, AccessToken: "synthetic-imported"})
	setKiloRestriction(a, v)
	m.mu.Lock()
	m.auths[a.ID] = a
	m.mu.Unlock()
	hook := &m6ClockHook{updates: make(chan *Auth, 10)}
	t.Cleanup(m.AddHook(hook))
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	m.startKiloRecovery(ctx, ticks, func() {})
	ticks <- due.Add(-time.Nanosecond)
	// The next unbuffered tick is received only after the previous cycle finishes.
	ticks <- due
	select {
	case <-hook.updates:
	case <-time.After(time.Second):
		t.Fatal("due host cycle did not publish")
	}
	if m.KiloRestriction(a.ID).Reason != "none" {
		t.Fatal("due did not clear")
	}
	cancel()
	m.StopKiloRecovery()
	v.Generation++
	v.Reason = kiloQuotaExhausted
	setKiloRestriction(a, v)
	m.mu.Lock()
	m.auths[a.ID] = a
	m.mu.Unlock()
	select {
	case ticks <- due.Add(time.Hour):
		t.Fatal("stopped host still consuming ticks")
	default:
	}
	m.ProcessKiloRecovery(ctx, due.Add(time.Hour))
	if m.KiloRestriction(a.ID).Reason != kiloQuotaExhausted {
		t.Fatal("cancelled cycle mutated state")
	}
	t.Log(fmt.Sprintf("controlled host ticks before=%s due=%s; stop prevents tasks", due.Add(-time.Nanosecond), due))
}
