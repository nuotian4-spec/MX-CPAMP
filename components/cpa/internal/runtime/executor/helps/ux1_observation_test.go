package helps

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"testing"
	"time"
)

func TestUX1ProxyObservationLifecycle(t *testing.T) {
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2, Proxies: []config.KiloProxy{{ID: "p", URL: "http://127.0.0.1:1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.StateView().Proxies[0]; got.ObservedExitIP != nil || got.ObservedAt != nil || got.ObservationStatus != "unknown" {
		t.Fatal(got)
	}
	p, _ := r.ProxyForProbe(0, "p", false)
	if _, err = r.CompleteProxyProbe(0, p, false, nil, "203.0.113.8"); err != nil {
		t.Fatal(err)
	}
	got := r.StateView().Proxies[0]
	if got.ObservedExitIP == nil || *got.ObservedExitIP != "203.0.113.8" || got.ObservedAt == nil || got.ObservationStatus != "ok" {
		t.Fatal(got)
	}
	r.markFailed("p")
	if got = r.StateView().Proxies[0]; got.ObservedExitIP == nil || got.ObservationStatus != "failed" {
		t.Fatal(got)
	}
	state := r.health["p"]
	state.healthy = true
	state.observedAt = time.Now().Add(-2 * time.Minute)
	r.health["p"] = state
	if r.StateView().Proxies[0].ObservationStatus != "stale" {
		t.Fatal("old observation is current")
	}
	next := r.state.Clone()
	next.Proxies[0].URL = "http://127.0.0.1:2"
	r.applyState(next)
	if got = r.StateView().Proxies[0]; got.ObservedExitIP != nil || got.ObservationStatus != "unknown" {
		t.Fatal(got)
	}
}
func TestUX1HealthPreparationOnlyUsesCandidates(t *testing.T) {
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2, Proxies: []config.KiloProxy{{ID: "selected", URL: "http://127.0.0.1:1"}, {ID: "other", URL: "http://127.0.0.1:2"}}}})
	if err != nil {
		t.Fatal(err)
	}
	next := r.state.Clone()
	next.Accounts["a"] = kilo.AccountEgress{Mode: "pool", CandidateProxyIDs: []string{"selected"}}
	r.applyState(next)
	r.health["selected"] = kiloProxyHealth{checked: true, healthy: false}
	if r.prepareHealth("a") || r.health["other"].pending {
		t.Fatal("unselected probe started")
	}
}

func TestUX1DeviceBusyPinDoesNotChooseSpare(t *testing.T) {
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 1, Proxies: []config.KiloProxy{{ID: "selected", URL: "http://127.0.0.1:1"}, {ID: "spare", URL: "http://127.0.0.1:2"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"selected", "spare"} {
		p, probeErr := r.ProxyForProbe(r.state.Revision, id, false)
		if probeErr != nil {
			t.Fatal(probeErr)
		}
		ip := []string{"203.0.113.9", "203.0.113.10"}[i]
		if _, err = r.CompleteProxyProbe(r.state.Revision, p, false, nil, ip); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _, err := r.PrepareDevice("device-owner", "new", kilo.AuthorizationEgress{Mode: "pool", ProxyID: "selected"}, kilo.InferenceEgress{Mode: "pool"}, cancel)
	if err != nil || a.ProxyID != "selected" {
		t.Fatal(err)
	}
	held, err := r.pool.TryAcquire("other", kilo.AcquireOptions{Transient: true, ProxyID: "selected"})
	if err != nil {
		t.Fatal(err)
	}
	if client, release, err := r.AcquireDevice("device-owner", "pool"); err == nil || client != nil || release != nil {
		t.Fatal("busy pin switched to spare")
	}
	if ctx.Err() != nil || r.devicePins["device-owner"] == nil {
		t.Fatal("busy lost pin")
	}
	held.Release()
	client, release, err := r.AcquireDevice("device-owner", "pool")
	if err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	release()
	if got := r.pool.Snapshot(); got.Proxies[0].Active != 0 || got.Proxies[1].Active != 0 {
		t.Fatal(got)
	}
	r.ReleaseDevice("device-owner")
	if r.proxyPinned("selected") {
		t.Fatal("released pin retained")
	}
}

func TestUX1PendingSavePreservesDraftAcrossOtherAllocation(t *testing.T) {
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2, Proxies: []config.KiloProxy{{ID: "shared", URL: "http://127.0.0.1:1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, draft, err := r.PrepareDevice("device-a", "reserved", kilo.AuthorizationEgress{Mode: "direct"}, kilo.InferenceEgress{Mode: "pool", PrimaryProxyID: "shared", CandidateProxyIDs: []string{"shared"}}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.BeginDeviceSave("device-a", "reserved", draft); err != nil {
		t.Fatal(err)
	}
	if _, err = r.persistAllocation("other", "shared", true); err != nil {
		t.Fatal(err)
	}
	if err = r.ValidateDeviceDraft("device-a", "reserved", draft); err != nil {
		t.Fatal(err)
	}
	if err = r.VerifyDeviceSave("reserved", draft); err != nil {
		t.Fatal("another shared allocation changed approved draft", err)
	}
	if r.state.Accounts["other"].PrimaryProxyID != "shared" {
		t.Fatal("unrelated allocation lost")
	}
}
