package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestIPAdmissionS2ExecutorLegacyDuplicateAdmission(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		name := "persisted"
		if failSave {
			name = "storage_failure"
		}
		t.Run(name, func(t *testing.T) {
			f := newIPAdmissionWire(t)
			at := time.Now().UTC()
			dir := t.TempDir()
			legacy := kilo.SavedProxy{ID: "legacy", URL: f.proxy.URL}
			registered := kilo.SavedProxy{ID: "registered", URL: "http://127.0.0.1:32103", Disabled: true, Order: 1, AdmissionStatus: "admitted", AdmittedIP: f.ip, AdmittedAt: at}
			state := kilo.ProxyState{Revision: 1, Limit: 2, Accounts: map[string]kilo.AccountEgress{"a": {Mode: "pool", PrimaryProxyID: legacy.ID, CandidateProxyIDs: []string{legacy.ID}}}, Proxies: []kilo.SavedProxy{legacy, registered}, IPHistory: map[string]kilo.ProxyIPRegistration{f.ip: {IP: f.ip, ProxyIDs: []string{registered.ID}, FirstAdmittedAt: &at, LastObservedAt: &at, Source: "admission"}}}
			if err := (kilo.ProxyStateStore{Dir: dir}).Save(state); err != nil {
				t.Fatal(err)
			}
			e := NewKiloExecutor(&config.Config{Kilo: config.KiloConfig{StateDir: dir}})
			if e.initErr != nil {
				t.Fatal(e.initErr)
			}
			e.mu.Lock()
			_, err := e.runtime.CompleteProxyProbe(1, legacy, false, nil)
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			auth := &coreauth.Auth{ID: "a", Provider: "kilo", Metadata: map[string]any{"allow_direct_when_proxy_full": true}}
			active, release, wait, err := e.TryAcquireEgress(auth)
			if err != nil || wait || active == nil || release == nil {
				t.Fatal("legacy setup lacks active resource", err)
			}
			defer release()
			path := filepath.Join(dir, kilo.StateFileName)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if failSave {
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(path); _ = os.WriteFile(path, original, 0600) })
			}
			before, _ := e.KiloProxyState()
			_, err = e.ProbeKiloProxy(context.Background(), before.Revision, legacy.ID, false)
			expected := kilo.ErrProxyIPAlreadyRegistered
			if failSave {
				expected = kilo.ErrStateStorage
			}
			if !errors.Is(err, expected) {
				t.Fatal("formal duplicate result", err)
			}
			blocked, rel, _, _ := e.TryAcquireEgress(auth)
			if blocked != nil || rel != nil {
				if rel != nil {
					rel()
				}
				t.Fatal("executor admitted a new duplicate lease")
			}
			view, _ := e.KiloProxyState()
			if view.Proxies[0].Healthy || view.Proxies[0].Active != 1 {
				t.Fatal("duplicate state/resource projection", view)
			}
			if failSave {
				if view.Revision != before.Revision || view.Proxies[0].AdmissionStatus != "legacy" || view.AutoError == nil || *view.AutoError != "storage_failed" {
					t.Fatal("failed persistence not exposed", view)
				}
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err = e.ProbeKiloProxy(context.Background(), view.Revision, legacy.ID, false); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
					t.Fatal(err)
				}
			} else if view.Proxies[0].AdmissionStatus != "pending" || view.Proxies[0].AdmissionReason != "duplicate_ip" {
				t.Fatal("duplicate quarantine missing", view)
			}
			release()
			if view, _ = e.KiloProxyState(); view.Proxies[0].Active != 0 {
				t.Fatal("held lease release lost")
			}
			loaded := NewKiloExecutor(&config.Config{Kilo: config.KiloConfig{StateDir: dir}})
			if loaded.initErr != nil {
				t.Fatal(loaded.initErr)
			}
			if blocked, rel, _, _ = loaded.TryAcquireEgress(auth); blocked != nil || rel != nil {
				if rel != nil {
					rel()
				}
				t.Fatal("restart admitted duplicate legacy")
			}
			if view, _ = loaded.KiloProxyState(); view.Proxies[0].AdmissionStatus != "pending" || view.Proxies[0].AdmissionReason != "duplicate_ip" {
				t.Fatal("restart projection lost quarantine", view)
			}
		})
	}
}
