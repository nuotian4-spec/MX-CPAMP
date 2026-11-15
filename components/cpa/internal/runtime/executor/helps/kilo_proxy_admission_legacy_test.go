package helps

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func admissionLegacyFixture(t *testing.T, archived, trusted bool) *KiloRuntime {
	t.Helper()
	p := kilo.SavedProxy{ID: "legacy", URL: "http://127.0.0.1:32101", FixedIP: ae1IP}
	group := kilo.IPGroup{IP: ae1IP, AccountIDs: []string{}}
	if trusted {
		group.AccountIDs = []string{"previous-account"}
	}
	state := kilo.ProxyState{Revision: 1, Limit: 2, MaxUniqueAccounts: 3, Accounts: map[string]kilo.AccountEgress{}, IPGroups: map[string]kilo.IPGroup{ae1IP: group}}
	if archived {
		state.Recycle = []kilo.ArchivedProxy{{Proxy: p, ArchivedAt: time.Now().UTC()}}
	} else {
		state.Proxies = []kilo.SavedProxy{p}
	}
	dir := t.TempDir()
	if err := (kilo.ProxyStateStore{Dir: dir}).Save(state); err != nil {
		t.Fatal(err)
	}
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIPAdmissionS1LegacyIdentitySurvivesPending(t *testing.T) {
	for _, action := range []string{"active_failure", "active_unknown", "active_private", "recycle_failure", "recycle_unknown"} {
		t.Run(action, func(t *testing.T) {
			archived := action == "recycle_failure" || action == "recycle_unknown"
			r := admissionLegacyFixture(t, archived, true)
			p, err := r.ProxyForProbe(r.state.Revision, "legacy", archived)
			if err != nil {
				t.Fatal(err)
			}
			probeErr, observed := kilo.ErrProxyHealth, ""
			if action == "active_unknown" || action == "recycle_unknown" {
				probeErr = nil
			}
			if action == "active_private" {
				probeErr, observed = nil, "192.168.1.2"
			}
			if _, err = r.CompleteProxyProbe(r.state.Revision, p, archived, probeErr, observed); !errors.Is(err, kilo.ErrProxyHealth) {
				t.Fatal(err)
			}
			reloaded, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: r.store.Dir}})
			if err != nil {
				t.Fatal(err)
			}
			r = reloaded
			p, err = r.ProxyForProbe(r.state.Revision, "legacy", archived)
			if err != nil || p.AdmissionState() != "pending" {
				t.Fatal("pending not persisted", p, err)
			}
			entry := r.state.IPHistory[ae1IP]
			if !slices.Contains(entry.ProxyIDs, p.ID) || entry.Source != "legacy_confirmed" || entry.FirstAdmittedAt != nil {
				t.Fatalf("trusted original identity lost before pending: %+v", entry)
			}
			if _, err = r.CompleteProxyProbe(r.state.Revision, p, archived, nil, ae1IP); err != nil {
				t.Fatal("original IP rejected after reload", err)
			}
			if len(r.state.IPGroups[ae1IP].AccountIDs) != 1 || r.state.IPGroups[ae1IP].Owner != "" {
				t.Fatal("recheck changed N or owner")
			}
			q := admissionCreate(t, r, "http://127.0.0.1:32103")
			revision := r.state.Revision
			if _, err = r.CompleteProxyProbe(revision, q, false, nil, ae1IP); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) || r.state.Revision != revision {
				t.Fatal("new ID reused trusted original identity", err)
			}
		})
	}
}

func TestIPAdmissionS1LegacyPendingSaveFailureIsAtomic(t *testing.T) {
	for _, action := range []string{"active", "recycle"} {
		t.Run(action, func(t *testing.T) {
			archived := action == "recycle"
			r := admissionLegacyFixture(t, archived, true)
			p, _ := r.ProxyForProbe(r.state.Revision, "legacy", archived)
			revision, dir := r.state.Revision, r.store.Dir
			bad := filepath.Join(t.TempDir(), "not-directory")
			if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			r.store.Dir = bad
			_, err := r.CompleteProxyProbe(revision, p, archived, kilo.ErrProxyHealth, "")
			if !errors.Is(err, kilo.ErrStateStorage) || r.state.Revision != revision || len(r.state.IPHistory) != 0 {
				t.Fatal("failed write partially migrated identity", err)
			}
			r.store.Dir = dir
			loaded, _, err := r.store.Load(kilo.ProxyState{})
			if err != nil || loaded.Revision != revision || len(loaded.IPHistory) != 0 || len(loaded.IPGroups[ae1IP].AccountIDs) != 1 {
				t.Fatal("failed write changed persisted history", err)
			}
			p, _ = r.ProxyForProbe(revision, p.ID, archived)
			if p.AdmissionState() != "legacy" {
				t.Fatal("failed write published pending")
			}
			if _, err = r.CompleteProxyProbe(revision, p, archived, nil, ae1IP); err != nil || len(r.state.IPGroups[ae1IP].AccountIDs) != 1 {
				t.Fatal("original restore failed after storage recovery", err)
			}
		})
	}
}

func TestIPAdmissionS1UntrustedFixedIPDoesNotSeedIdentity(t *testing.T) {
	for _, pending := range []bool{false, true} {
		r := admissionLegacyFixture(t, false, false)
		if pending {
			next := r.state.Clone()
			next.Proxies[0].AdmissionStatus, next.Proxies[0].AdmissionReason = "pending", "unverified"
			g := next.IPGroups[ae1IP]
			g.AccountIDs = []string{"previous-account"}
			next.IPGroups[ae1IP] = g
			if _, err := r.commitState(next); err != nil {
				t.Fatal(err)
			}
		}
		p, _ := r.ProxyForProbe(r.state.Revision, "legacy", false)
		if _, err := r.CompleteProxyProbe(r.state.Revision, p, false, kilo.ErrProxyHealth, ""); !errors.Is(err, kilo.ErrProxyHealth) {
			t.Fatal(err)
		}
		if slices.Contains(r.state.IPHistory[ae1IP].ProxyIDs, p.ID) {
			t.Fatal("untrusted fixed IP or pending ID gained original identity")
		}
	}
}

func TestIPAdmissionS1FixedConnectionEditKeepsExistingConflict(t *testing.T) {
	r := admissionLegacyFixture(t, false, true)
	p, _ := r.ProxyForProbe(r.state.Revision, "legacy", false)
	if _, err := r.CompleteProxyProbe(r.state.Revision, p, false, nil); err != nil {
		t.Fatal(err)
	}
	before := r.StateView()
	original, err := os.ReadFile(filepath.Join(r.store.Dir, kilo.StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	url := "http://127.0.0.1:32102"
	if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: p.ID, URL: &url}, nil); !errors.Is(err, kilo.ErrStateConflict) {
		t.Fatal("fixed connection edit gate changed", err)
	}
	after := r.StateView()
	stored, err := os.ReadFile(filepath.Join(r.store.Dir, kilo.StateFileName))
	if err != nil || string(original) != string(stored) || after.Revision != before.Revision || after.Proxies[0].Healthy != before.Proxies[0].Healthy || after.Proxies[0].AdmissionStatus != "legacy" || len(r.state.IPHistory) != 0 {
		t.Fatal("rejected fixed connection edit changed state, history or health", err)
	}
}

func admissionLegacyDuplicateRuntime(t *testing.T, url string) (*KiloRuntime, kilo.SavedProxy) {
	t.Helper()
	r := admissionRuntime(t)
	q := admissionFinish(t, r, admissionCreate(t, r, "http://127.0.0.1:32103"), ae1IP)
	enabled := false
	if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: q.ID, Enabled: &enabled}, nil); err != nil {
		t.Fatal(err)
	}
	next := r.state.Clone()
	next.Proxies = append(next.Proxies, kilo.SavedProxy{ID: "legacy", URL: url, Order: 1})
	next.Accounts["a"] = kilo.AccountEgress{Mode: "pool", PrimaryProxyID: "legacy", CandidateProxyIDs: []string{"legacy"}}
	if _, err := r.commitState(next); err != nil {
		t.Fatal(err)
	}
	p, err := r.ProxyForProbe(r.state.Revision, "legacy", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CompleteProxyProbe(r.state.Revision, p, false, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.state.IPGroups) != 0 || len(r.state.IPHistory) != 1 {
		t.Fatal("fixture requires history-only registered IP")
	}
	return r, p
}

func admissionLegacyBlocked(t *testing.T, r *KiloRuntime) {
	t.Helper()
	for _, p := range r.StateView().Proxies {
		if p.ID == "legacy" && p.Healthy {
			t.Fatal("formally rejected legacy proxy still healthy")
		}
	}
	for _, auth := range []*coreauth.Auth{
		{ID: "a", Provider: "kilo", Metadata: map[string]any{"allow_direct_when_proxy_full": true}},
		{ID: "shared", Provider: "kilo"},
	} {
		if client, release, _, _ := r.TryAcquire(auth); client != nil || release != nil {
			if release != nil {
				release()
			}
			t.Fatal("duplicate legacy supplied new resources or full direct fallback")
		}
	}
	if _, _, err := r.PrepareDevice("rejected-device", "new-account", kilo.AuthorizationEgress{Mode: "pool", ProxyID: "legacy"}, kilo.InferenceEgress{Mode: "pool"}, func() {}); err == nil {
		t.Error("duplicate legacy supplied explicit authorization")
	}
	for _, draft := range []kilo.InferenceEgress{{Mode: "pool", PrimaryProxyID: "legacy"}, {Mode: "pool", CandidateProxyIDs: []string{"legacy"}}, {Mode: "pool", ExclusiveProxyIDs: []string{"legacy"}}} {
		if _, _, err := r.PrepareDevice("rejected-inference", "a", kilo.AuthorizationEgress{Mode: "direct"}, draft, func() {}); err == nil {
			t.Error("duplicate legacy supplied inference draft")
		}
	}
	if client, release, err := r.AcquireDevice("unprepared-device", "pool"); err == nil || client != nil || release != nil {
		if release != nil {
			release()
		}
		t.Fatal("duplicate legacy supplied device capacity")
	}
	capacity := 0
	for _, p := range r.Snapshot().Proxies {
		if p.Healthy {
			capacity += p.Limit
		}
	}
	if capacity != 0 {
		t.Fatal("duplicate legacy contributed schedulable pool capacity", capacity)
	}
}

func TestIPAdmissionS2LegacyDuplicateBlocksResourcesAndRestart(t *testing.T) {
	f := admissionProxy(t)
	r, _ := admissionLegacyDuplicateRuntime(t, f.proxy.URL)
	initialCapacity := 0
	for _, proxy := range r.Snapshot().Proxies {
		if proxy.Healthy {
			initialCapacity += proxy.Limit
		}
	}
	if initialCapacity != 2 {
		t.Fatal("fixture lacks real healthy capacity", initialCapacity)
	}
	auth := &coreauth.Auth{ID: "a", Provider: "kilo"}
	client, release, wait, err := r.TryAcquire(auth)
	if err != nil || wait || client == nil || release == nil {
		t.Fatal("unreviewed legacy compatibility changed", err)
	}
	defer release()
	p, _ := r.ProxyForProbe(r.state.Revision, "legacy", false)
	if _, err = r.CompleteProxyProbe(r.state.Revision, p, false, nil, ae1IP); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
		t.Fatal(err)
	}
	admissionLegacyBlocked(t, r)
	p, _ = r.ProxyForProbe(r.state.Revision, p.ID, false)
	if p.AdmissionState() != "pending" || p.AdmissionReason != "duplicate_ip" || len(r.state.IPHistory) != 1 || len(r.state.IPGroups) != 0 {
		t.Fatal("duplicate quarantine/history not committed", p)
	}
	active := 0
	for _, p := range r.Snapshot().Proxies {
		active += p.Active
	}
	if active != 1 {
		t.Fatal("duplicate quarantine killed active lease")
	}
	release()
	for _, p := range r.Snapshot().Proxies {
		if p.Active != 0 {
			t.Fatal("held lease did not release")
		}
	}
	r.SetObservationCommit(func(id, raw string, generation uint64) {
		if err := r.CommitProxyObservation(id, raw, generation); err != nil {
			t.Error(err)
		}
	})
	r.probe(p.ID, p.URL, r.health[p.ID].generation)
	admissionLegacyBlocked(t, r)
	reloaded, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: r.store.Dir}})
	if err != nil {
		t.Fatal(err)
	}
	admissionLegacyBlocked(t, reloaded)
	loaded, _ := reloaded.ProxyForProbe(reloaded.state.Revision, p.ID, false)
	if loaded.AdmissionState() != "pending" || loaded.AdmissionReason != "duplicate_ip" {
		t.Fatal("restart lost duplicate quarantine", loaded)
	}
}

func TestIPAdmissionS2LegacyDuplicateSaveFailureKeepsMemoryFence(t *testing.T) {
	f := admissionProxy(t)
	r, p := admissionLegacyDuplicateRuntime(t, f.proxy.URL)
	_, draft, err := r.PrepareDevice("existing-device", "device-account", kilo.AuthorizationEgress{Mode: "pool", ProxyID: p.ID}, kilo.InferenceEgress{Mode: "pool", PrimaryProxyID: p.ID}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	client, release, err := r.AcquireDevice("existing-device", "pool")
	if err != nil || client == nil || release == nil {
		t.Fatal("existing pin lacks resource", err)
	}
	defer release()
	p, _ = r.ProxyForProbe(r.state.Revision, p.ID, false)
	dir, revision := r.store.Dir, r.state.Revision
	bad := filepath.Join(t.TempDir(), "not-directory")
	if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	r.store.Dir = bad
	if _, err := r.CompleteProxyProbe(revision, p, false, nil, ae1IP); !errors.Is(err, kilo.ErrStateStorage) {
		t.Fatal("duplicate save failure not surfaced", err)
	}
	admissionLegacyBlocked(t, r)
	if err := r.ValidateDeviceDraft("existing-device", "device-account", draft); err == nil {
		t.Error("existing inference draft ignored memory fence")
	}
	if client, nextRelease, err := r.AcquireDevice("existing-device", "pool"); err == nil || client != nil || nextRelease != nil {
		if nextRelease != nil {
			nextRelease()
		}
		t.Error("existing pin acquired after duplicate fence")
	}
	if r.Snapshot().Proxies[1].Active != 1 {
		t.Fatal("save failure killed held lease")
	}
	release()
	if r.Snapshot().Proxies[1].Active != 0 {
		t.Fatal("held lease did not release after failed save")
	}
	if r.state.Revision != revision || r.state.Proxies[1].AdmissionState() != "legacy" || r.autoError == nil || *r.autoError != "storage_failed" {
		t.Fatal("failed commit misreported persistence")
	}
	r.probe(p.ID, p.URL, r.health[p.ID].generation)
	admissionLegacyBlocked(t, r)
	r.store.Dir = dir
	p, _ = r.ProxyForProbe(r.state.Revision, p.ID, false)
	if _, err := r.CompleteProxyProbe(r.state.Revision, p, false, nil, ae1IP); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
		t.Fatal("retry did not persist duplicate", err)
	}
	if r.autoError != nil {
		t.Fatal("successful pending retry retained storage error")
	}
	reloaded, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	admissionLegacyBlocked(t, reloaded)
}

func TestIPAdmissionS2UnreviewedLegacyKeepsCompatibility(t *testing.T) {
	f := admissionProxy(t)
	r, p := admissionLegacyDuplicateRuntime(t, f.proxy.URL)
	r.probe(p.ID, p.URL, r.health[p.ID].generation)
	if p, _ = r.ProxyForProbe(r.state.Revision, p.ID, false); p.AdmissionState() != "legacy" {
		t.Fatal("background migrated unreviewed legacy")
	}
	client, release, wait, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
	if err != nil || wait || client == nil || release == nil {
		t.Fatal("unreviewed legacy was batch quarantined", err)
	}
	release()
}
