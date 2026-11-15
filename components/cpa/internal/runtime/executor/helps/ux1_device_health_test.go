package helps

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

func TestUX1DeviceStaleObservationDoesNotBlockOrProbe(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		name := "automatic"
		if explicit {
			name = "explicit"
		}
		t.Run(name, func(t *testing.T) {
			r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 1, Proxies: []config.KiloProxy{{ID: "selected", URL: "http://127.0.0.1:1"}}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = r.CompleteProxyProbe(0, r.state.Proxies[0], false, nil, "203.0.113.9"); err != nil {
				t.Fatal(err)
			}
			stale := time.Now().Add(-2 * time.Minute)
			health := r.health["selected"]
			health.checkedAt, health.observedAt = stale, stale
			r.health["selected"] = health
			sequence := r.probeSequence
			authorization := kilo.AuthorizationEgress{Mode: "pool"}
			if explicit {
				authorization.ProxyID = "selected"
			}
			pinned, _, err := r.PrepareDevice("device-owner", "reserved", authorization, kilo.InferenceEgress{Mode: "pool"}, func() {})
			if err != nil || pinned.ProxyID != "selected" {
				t.Fatalf("stale observation blocked device start: pin=%+v err=%v", pinned, err)
			}
			defer r.ReleaseDevice("device-owner")
			for _, stage := range []string{"start", "poll"} {
				client, release, err := r.AcquireDevice("device-owner", "pool")
				if err != nil {
					t.Fatalf("%s rejected known healthy proxy: %v", stage, err)
				}
				client.CloseIdleConnections()
				release()
				view := r.StateView().Proxies[0]
				if view.Active != 0 || view.ObservationStatus != "stale" || r.probeSequence != sequence || r.health["selected"].pending || !r.health["selected"].checkedAt.Equal(stale) {
					t.Fatalf("%s changed observation or retained lease: %+v", stage, view)
				}
			}
		})
	}
}

func TestUX1DevicePollUnhealthyPinDoesNotProbeOrChooseSpare(t *testing.T) {
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
	if _, _, err = r.PrepareDevice("device-owner", "reserved", kilo.AuthorizationEgress{Mode: "pool", ProxyID: "selected"}, kilo.InferenceEgress{Mode: "pool"}, func() {}); err != nil {
		t.Fatal(err)
	}
	defer r.ReleaseDevice("device-owner")
	r.markFailed("selected")
	for _, checked := range []bool{true, false} {
		health := r.health["selected"]
		health.checked = checked
		r.health["selected"] = health
		sequence := r.probeSequence
		client, release, err := r.AcquireDevice("device-owner", "pool")
		if err == nil || client != nil || release != nil {
			t.Fatal("unhealthy pin switched to spare or direct")
		}
		if r.probeSequence != sequence || r.health["selected"].pending {
			t.Fatal("poll started another IP probe")
		}
	}
	for _, proxy := range r.Snapshot().Proxies {
		if proxy.Active != 0 {
			t.Fatal("failed poll retained a lease")
		}
	}
}
