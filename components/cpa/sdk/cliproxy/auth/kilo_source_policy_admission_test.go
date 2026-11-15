package auth

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestAE1SourcePolicyPublishesFormalScheduler(t *testing.T) {
	m, a := m6Manager(t)
	key := "second-synthetic-key"
	b, err := m.ChangeKiloAccount(context.Background(), "", KiloAccountChanges{APIKey: &key}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := kilo.NewProxyPool([]kilo.Proxy{{ID: "proxy", Healthy: true}}, 2)
	e := &noReplayTestExecutor{pool: pool}
	m.RegisterExecutor(e)
	model := t.Name()
	for _, id := range []string{a.ID, b.ID} {
		registry.GetGlobalRegistry().RegisterClient(id, "kilo", []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
		m.RefreshSchedulerEntry(id)
	}
	ctx := channelscope.With(context.Background(), "kilo")
	if _, err := m.ChangeKiloSourcePolicy(ctx, a.ID, 0, true, 1800, false); err != nil {
		t.Fatal(err)
	}
	ae1Observe(t, m, a.ID, "100", true, nil)
	selected, _, _, lease, err := m.pickNextMixedAdmitted(ctx, []string{"kilo"}, model, core.Options{}, nil)
	if err != nil || selected.ID != b.ID {
		t.Fatalf("paused account displaced healthy account: %v %v", selected, err)
	}
	lease.release()
	opts := core.Options{Metadata: map[string]any{core.PinnedAuthMetadataKey: a.ID}}
	_, _, _, _, err = m.pickNextMixedAdmitted(ctx, []string{"kilo"}, model, opts, nil)
	if err == nil || e.acquired.Load() != 1 {
		t.Fatalf("paused account acquired egress: %v %d", err, e.acquired.Load())
	}
	for _, disable := range []bool{false, true} {
		if disable {
			ae1Observe(t, m, a.ID, "100", true, nil)
			v, _ := m.KiloSourcePolicy(a.ID)
			if _, err = m.ChangeKiloSourcePolicy(ctx, a.ID, v.Revision, false, 1800, false); err != nil {
				t.Fatal(err)
			}
		} else {
			ae1Observe(t, m, a.ID, "1", true, nil)
		}
		selected, _, _, lease, err = m.pickNextMixedAdmitted(ctx, []string{"kilo"}, model, opts, nil)
		if err != nil || selected.ID != a.ID {
			t.Fatalf("recovered scheduler remained blocked: %v %v", selected, err)
		}
		lease.release()
	}
}

func TestAE1SourceReplacementCannotClearPauseAtCommit(t *testing.T) {
	for _, replace := range []bool{false, true} {
		m, a := m6Manager(t)
		m.mu.Lock()
		source := &Auth{ID: "source", Provider: "codex", Status: StatusActive, RegistrationEpoch: 1, CredentialVersion: 1, Metadata: map[string]any{"account_id": "outer", "access_token": "synthetic"}}
		m.auths[source.ID] = source
		m.mu.Unlock()
		if _, err := m.ChangeKiloSourcePolicy(context.Background(), a.ID, 0, true, 1800, false); err != nil {
			t.Fatal(err)
		}
		ae1Observe(t, m, a.ID, "100", true, nil)
		live, _ := m.GetByID(a.ID)
		p := kiloSourcePolicy(live)
		ticket := kiloPolicyTicket{revision: p.Revision, pause: p.PauseGeneration, identity: kiloIdentityRevision(live), epoch: live.RegistrationEpoch, started: time.Now(), source: codexUsageSnapshot(source, "outer")}
		releaseTarget := m.lockAuthMutation(a.ID)
		done := make(chan struct{})
		go func() { ae1Observe(t, m, a.ID, "1", true, &ticket); close(done) }()
		m.Remove(context.Background(), source.ID)
		if replace {
			m.mu.Lock()
			next := source.Clone()
			next.RegistrationEpoch = 2
			next.Metadata["access_token"] = "replacement"
			m.auths[source.ID] = next
			m.mu.Unlock()
		}
		releaseTarget()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("policy callback did not finish")
		}
		v, _ := m.KiloSourcePolicy(a.ID)
		if !v.Paused || v.Status != "source_pending" {
			t.Fatalf("stale source cleared pause: %+v", v)
		}
	}
}
