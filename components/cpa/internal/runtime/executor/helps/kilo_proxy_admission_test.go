package helps

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type admissionWire struct {
	mu    sync.Mutex
	ip    string
	fail  bool
	proxy *httptest.Server
}

func admissionProxy(t *testing.T) *admissionWire {
	t.Helper()
	f := &admissionWire{ip: "203.0.113.10"}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Host != "api.ipify.org" {
			t.Errorf("unexpected observation request %s %s", r.Method, r.Host)
			http.Error(w, "unexpected", 400)
			return
		}
		f.mu.Lock()
		ip, fail := f.ip, f.fail
		f.mu.Unlock()
		if fail {
			http.Error(w, "synthetic failure", 502)
			return
		}
		_, _ = io.WriteString(w, ip)
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	original := http.DefaultTransport
	tr := original.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: upstream.Certificate().DNSNames[0]}
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = original; tr.CloseIdleConnections() })
	f.proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "api.ipify.org:443" {
			t.Errorf("unexpected proxy target %s %s", r.Method, r.Host)
			http.Error(w, "unexpected", 400)
			return
		}
		remote, err := net.Dial("tcp", upstream.Listener.Addr().String())
		if err != nil {
			http.Error(w, "local target", 502)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = remote.Close()
			return
		}
		defer client.Close()
		defer remote.Close()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(remote, client); _ = remote.Close() }()
		_, _ = io.Copy(client, remote)
	}))
	t.Cleanup(f.proxy.Close)
	return f
}
func admissionRuntime(t *testing.T) *KiloRuntime {
	t.Helper()
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func admissionCreate(t *testing.T, r *KiloRuntime, url string) kilo.SavedProxy {
	t.Helper()
	view, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, Create: true, URL: &url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.ProxyForProbe(view.Revision, view.Proxies[len(view.Proxies)-1].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func admissionFinish(t *testing.T, r *KiloRuntime, p kilo.SavedProxy, ip string) kilo.SavedProxy {
	t.Helper()
	p, err := r.ProxyForProbe(r.state.Revision, p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CompleteProxyProbe(r.state.Revision, p, false, nil, ip); err != nil {
		t.Fatal(err)
	}
	p, _ = r.ProxyForProbe(r.state.Revision, p.ID, false)
	return p
}
func admissionNoResources(t *testing.T, r *KiloRuntime, p kilo.SavedProxy) {
	t.Helper()
	if r.StateView().Proxies[0].Healthy {
		t.Fatal("pending proxy healthy")
	}
	if lease, err := r.pool.TryAcquire("a", kilo.AcquireOptions{AllowDirectWhenProxyFull: true}); lease != nil || !errors.Is(err, kilo.ErrProxyUnavailable) {
		t.Fatalf("pending shared/direct capacity %v %v", lease, err)
	}
	if a, release, _, _ := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo", Metadata: map[string]any{"allow_direct_when_proxy_full": true}}); a != nil || release != nil {
		t.Fatal("pending inferred capacity")
	}
	if client, release, err := r.AcquireDevice("new-device", "pool"); client != nil || release != nil || err == nil {
		t.Fatal("pending device resource")
	}
	for _, draft := range []kilo.InferenceEgress{{Mode: "pool", PrimaryProxyID: p.ID}, {Mode: "pool", CandidateProxyIDs: []string{p.ID}}, {Mode: "pool", ExclusiveProxyIDs: []string{p.ID}}} {
		if _, _, err := r.PrepareDevice("new-device", "new-account", kilo.AuthorizationEgress{Mode: "direct"}, draft, func() {}); err == nil {
			t.Fatal("pending inference candidate")
		}
	}
	if _, _, err := r.PrepareDevice("new-device", "new-account", kilo.AuthorizationEgress{Mode: "pool", ProxyID: p.ID}, kilo.InferenceEgress{Mode: "pool"}, func() {}); err == nil {
		t.Fatal("pending authorization candidate")
	}
	if len(r.devicePins) != 0 || len(r.state.IPHistory) != 0 {
		t.Fatal("pending acquired pin/history")
	}
}
func TestIPAdmissionPendingLifecycleAndReadOnlyObservation(t *testing.T) {
	f := admissionProxy(t)
	r := admissionRuntime(t)
	p := admissionCreate(t, r, f.proxy.URL)
	ip, err := ProbeKiloProxyObservation(context.Background(), p.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.10" || len(r.state.IPHistory) != 0 || r.state.Revision != 1 {
		t.Fatal("read-only observation reserved IP")
	}
	admissionNoResources(t, r, p)
	r.mu.Lock()
	r.probeSequence++
	g := r.probeSequence
	r.health[p.ID] = kiloProxyHealth{pending: true, generation: g}
	r.mu.Unlock()
	r.probe(p.ID, p.URL, g)
	admissionNoResources(t, r, p)
	enabled := true
	if _, err = r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: p.ID, Enabled: &enabled}, nil); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: r.store.Dir}})
	if err != nil {
		t.Fatal(err)
	}
	admissionNoResources(t, reloaded, p)
	p = admissionFinish(t, r, p, "::ffff:203.0.113.10")
	if p.AdmittedIP != "203.0.113.10" || len(r.state.IPHistory) != 1 || !r.StateView().Proxies[0].Healthy {
		t.Fatal("formal admission missing")
	}
	if len(r.state.IPGroups) != 0 {
		t.Fatal("admission fabricated account history")
	}
}
func TestIPAdmissionDuplicateHistorySurvivesClearAndRestart(t *testing.T) {
	r := admissionRuntime(t)
	p := admissionCreate(t, r, "http://127.0.0.1:32101")
	p = admissionFinish(t, r, p, "203.0.113.10")
	if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: p.ID, Archive: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ClearRecycle: true}, nil); err != nil {
		t.Fatal(err)
	}
	r2, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: r.store.Dir}})
	if err != nil {
		t.Fatal(err)
	}
	q := admissionCreate(t, r2, "http://127.0.0.1:32102")
	revision := r2.state.Revision
	_, err = r2.CompleteProxyProbe(revision, q, false, nil, "::ffff:203.0.113.10")
	if !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) || r2.state.Revision != revision || len(r2.state.IPHistory) != 1 || r2.StateView().Proxies[0].Healthy {
		t.Fatalf("duplicate changed state: %v", err)
	}
}
func TestIPAdmissionSaveFailureCancelAndStaleRevision(t *testing.T) {
	r := admissionRuntime(t)
	p := admissionCreate(t, r, "http://127.0.0.1:32101")
	revision := r.state.Revision
	dir := r.store.Dir
	bad := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	r.store.Dir = bad
	if _, err := r.CompleteProxyProbe(revision, p, false, nil, "203.0.113.10"); !errors.Is(err, kilo.ErrStateStorage) {
		t.Fatal(err)
	}
	if len(r.state.IPHistory) != 0 || r.state.Revision != revision || r.StateView().Proxies[0].Healthy {
		t.Fatal("save failure half admitted")
	}
	r.store.Dir = dir
	if _, err := r.CompleteProxyProbe(revision, p, false, context.Canceled, "203.0.113.10"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := r.CompleteProxyProbe(revision+1, p, false, nil, "203.0.113.10"); !errors.Is(err, kilo.ErrStateConflict) {
		t.Fatal(err)
	}
	changed := p
	changed.URL = "http://127.0.0.1:32102"
	if _, err := r.CompleteProxyProbe(revision, changed, false, nil, "203.0.113.10"); !errors.Is(err, kilo.ErrStateConflict) {
		t.Fatal(err)
	}
	if len(r.state.IPHistory) != 0 || r.state.Revision != revision {
		t.Fatal("cancel/stale reserved history")
	}
	admissionFinish(t, r, p, "203.0.113.10")
}
func TestIPAdmissionBackgroundQuarantinePersistsAndDoesNotReactivate(t *testing.T) {
	f := admissionProxy(t)
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "drift", true: "failure"}[failure], func(t *testing.T) {
			r := admissionRuntime(t)
			p := admissionFinish(t, r, admissionCreate(t, r, f.proxy.URL), "203.0.113.10")
			r.SetObservationCommit(func(id, raw string, generation uint64) {
				if err := r.CommitProxyObservation(id, raw, generation); err != nil {
					t.Error(err)
				}
			})
			f.mu.Lock()
			f.ip = "203.0.113.20"
			f.fail = failure
			f.mu.Unlock()
			r.mu.Lock()
			r.probeSequence++
			g := r.probeSequence
			h := r.health[p.ID]
			h.pending = true
			h.generation = g
			r.health[p.ID] = h
			r.mu.Unlock()
			r.probe(p.ID, p.URL, g)
			view := r.StateView()
			reason := "ip_changed"
			if failure {
				reason = "probe_failed"
			}
			if view.Proxies[0].AdmissionStatus != "pending" || view.Proxies[0].AdmissionReason != reason || view.Proxies[0].Healthy {
				t.Fatal(view)
			}
			reloaded, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: r.store.Dir}})
			if err != nil || reloaded.StateView().Proxies[0].AdmissionStatus != "pending" {
				t.Fatalf("restart quarantine %v", err)
			}
			f.mu.Lock()
			f.ip = "203.0.113.10"
			f.fail = false
			f.mu.Unlock()
			r.mu.Lock()
			r.probeSequence++
			g = r.probeSequence
			h = r.health[p.ID]
			h.generation = g
			r.health[p.ID] = h
			r.mu.Unlock()
			r.probe(p.ID, p.URL, g)
			if r.StateView().Proxies[0].Healthy {
				t.Fatal("background recovery bypassed admission")
			}
			admissionFinish(t, r, p, "203.0.113.10")
		})
	}
}
func TestIPAdmissionPendingCapacityAndOriginalNAccountRestore(t *testing.T) {
	r, accounts := ae1AutoRuntime(t, 2)
	r.ReconcileAuto(accounts, time.Now())
	p, _ := r.ProxyForProbe(r.state.Revision, "p1", false)
	if _, err := r.CompleteProxyProbe(r.state.Revision, p, false, nil, "203.0.113.99"); !errors.Is(err, kilo.ErrProxyHealth) {
		t.Fatal(err)
	}
	view := r.StateView()
	for _, entry := range view.Proxies {
		if entry.ID == "p1" && entry.Healthy {
			t.Fatal("pending entry healthy")
		}
	}
	p = admissionFinish(t, r, p, ae1IP)
	next := r.state.Clone()
	returnAutoGroup(&next, ae1IP, time.Now())
	if _, err := r.commitState(next); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: p.ID, Archive: true}, accounts); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: r.store.Dir}})
	if err != nil {
		t.Fatal(err)
	}
	r = reloaded
	p, err = r.ProxyForProbe(r.state.Revision, p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CompleteProxyProbe(r.state.Revision, p, true, nil, ae1IP); err != nil {
		t.Fatalf("original IP rejected: %v", err)
	}
	if len(r.state.IPGroups[ae1IP].AccountIDs) != 1 {
		t.Fatal("restore changed N account count")
	}
	if _, err = r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: p.ID, Archive: true}, accounts); err != nil {
		t.Fatal(err)
	}
	next = r.state.Clone()
	g := next.IPGroups[ae1IP]
	g.AccountIDs = []string{"a", "b"}
	g.Retired = true
	next.IPGroups[ae1IP] = g
	if _, err = r.commitState(next); err != nil {
		t.Fatal(err)
	}
	p, _ = r.ProxyForProbe(r.state.Revision, p.ID, true)
	if _, err = r.CompleteProxyProbe(r.state.Revision, p, true, nil, ae1IP); !errors.Is(err, kilo.ErrStateConflict) {
		t.Fatal("N limit bypass", err)
	}
}
func TestIPAdmissionLegacyTrustAndDifferentIPDuplicate(t *testing.T) {
	r := admissionRuntime(t)
	next := r.state.Clone()
	next.IPGroups = map[string]kilo.IPGroup{"203.0.113.10": {IP: "203.0.113.10", AccountIDs: []string{}}}
	next.Proxies = []kilo.SavedProxy{{ID: "old", URL: "http://127.0.0.1:32101", FixedIP: "203.0.113.10"}}
	if _, err := r.commitState(next); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := r.store.Load(kilo.ProxyState{})
	if err != nil || len(loaded.IPHistory) != 0 {
		t.Fatal("hand-entered IP backfilled history", err)
	}
	p := admissionCreate(t, r, "http://127.0.0.1:32102")
	p = admissionFinish(t, r, p, "203.0.113.20")
	q := admissionCreate(t, r, "http://127.0.0.1:32103")
	q = admissionFinish(t, r, q, "203.0.113.30")
	if _, err = r.CompleteProxyProbe(r.state.Revision, q, false, nil, p.AdmittedIP); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
		t.Fatal(err)
	}
	if r.state.Proxies[2].AdmissionState() != "pending" || len(r.state.IPHistory) != 2 {
		t.Fatal("different-IP duplicate bypass")
	}
	trusted := r.state.Clone()
	g := trusted.IPGroups["203.0.113.10"]
	g.AccountIDs = []string{"previous-account"}
	trusted.IPGroups[g.IP] = g
	if _, err = r.commitState(trusted); err != nil {
		t.Fatal(err)
	}
	x := admissionCreate(t, r, "http://127.0.0.1:32104")
	if _, err = r.CompleteProxyProbe(r.state.Revision, x, false, nil, "203.0.113.10"); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
		t.Fatal("trusted old history ignored", err)
	}
	if len(r.state.IPHistory) != 2 {
		t.Fatal("duplicate check wrote migration")
	}
}
func TestIPAdmissionPinQuarantinePreservesActiveLease(t *testing.T) {
	r := admissionRuntime(t)
	p := admissionFinish(t, r, admissionCreate(t, r, "http://127.0.0.1:32101"), "203.0.113.10")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, draft, err := r.PrepareDevice("device", "account", kilo.AuthorizationEgress{Mode: "pool", ProxyID: p.ID}, kilo.InferenceEgress{Mode: "pool", PrimaryProxyID: p.ID}, cancel)
	if err != nil {
		t.Fatal(err)
	}
	client, release, err := r.AcquireDevice("device", "pool")
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	p, _ = r.ProxyForProbe(r.state.Revision, p.ID, false)
	if _, err = r.CompleteProxyProbe(r.state.Revision, p, false, kilo.ErrProxyHealth); !errors.Is(err, kilo.ErrProxyHealth) {
		t.Fatal(err)
	}
	if ctx.Err() == nil || r.ValidateDeviceDraft("device", "account", draft) == nil {
		t.Fatal("pending pin remained valid")
	}
	if c, rel, err := r.AcquireDevice("device", "pool"); c != nil || rel != nil || err == nil {
		t.Fatal("pending pin acquired or switched")
	}
	if r.pool.Snapshot().Proxies[0].Active != 1 {
		t.Fatal("quarantine killed active lease")
	}
	release()
	if r.pool.Snapshot().Proxies[0].Active != 0 {
		t.Fatal("lease not released")
	}
}

func TestIPAdmissionPendingIPCapacityAndNoNewGroupPoison(t *testing.T) {
	for _, mode := range []string{"auto", "specified"} {
		t.Run(mode, func(t *testing.T) {
			r, accounts := ux2Runtime(t, 1)
			ip := "203.0.113.1"
			for _, id := range []string{"p0", "alias"} {
				p, err := r.ProxyForProbe(r.state.Revision, id, false)
				if err != nil {
					t.Fatal(err)
				}
				admissionFinish(t, r, p, ip)
			}
			policy := kilo.CapacityPolicy{CapacityMode: "ip", AllocationMode: mode, MaxIPs: 1}
			if mode == "specified" {
				policy.SpecifiedIPs = []string{ip}
			}
			if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, EgressAccount: "a", Mode: "auto", CapacityPolicy: policy}, accounts); err != nil {
				t.Fatal(err)
			}
			capacity := func() int {
				for _, a := range r.StateView().Accounts {
					if a.AccountID == "a" {
						if a.CapacityMode != "ip" || a.ConfiguredCapacity != 20 {
							t.Fatalf("missing IP capacity policy: %+v", a)
						}
						return a.CurrentCapacity
					}
				}
				t.Fatal("account capacity missing")
				return -1
			}
			if capacity() != 20 || len(r.state.IPGroups[ip].AccountIDs) != 1 {
				t.Fatal("initial capacity/history not established")
			}
			url := "http://127.0.0.1:32109"
			revision := r.state.Revision
			if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: revision, Create: true, URL: &url, FixedIP: &ip}, accounts); err == nil || r.state.Revision != revision || capacity() != 20 {
				t.Fatal("new fixed-IP input joined or poisoned the trusted group")
			}
			newProxy := admissionCreate(t, r, url)
			revision = r.state.Revision
			if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: revision, ID: newProxy.ID, FixedIP: &ip}, accounts); !errors.Is(err, kilo.ErrProxyHealth) {
				t.Fatal("pending row accepted fixed-IP membership", err)
			}
			if _, err := r.CompleteProxyProbe(revision, newProxy, false, nil, ip); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
				t.Fatal("new duplicate admitted", err)
			}
			newProxy, _ = r.ProxyForProbe(r.state.Revision, newProxy.ID, false)
			if newProxy.FixedIP != "" || r.state.Revision != revision || capacity() != 20 || len(r.state.IPGroups[ip].AccountIDs) != 1 {
				t.Fatal("duplicate changed trusted membership, capacity, revision, or N")
			}
			auth := &coreauth.Auth{ID: "a", Provider: "kilo", Metadata: map[string]any{"allow_direct_when_proxy_full": true}}
			client, release, wait, err := r.TryAcquire(auth)
			if err != nil || wait || client == nil || release == nil {
				t.Fatal("duplicate poisoned the admitted group", err)
			}
			release()
			p, _ := r.ProxyForProbe(r.state.Revision, "alias", false)
			if _, err = r.CompleteProxyProbe(r.state.Revision, p, false, nil, "203.0.113.99"); !errors.Is(err, kilo.ErrProxyHealth) {
				t.Fatal("trusted alias drift did not quarantine", err)
			}
			if capacity() != 0 || r.autoGroupHealthy(ip) {
				t.Fatal("pending trusted alias contributed group capacity")
			}
			if client, release, wait, err = r.TryAcquire(auth); err != nil || !wait || client != nil || release != nil {
				t.Fatal("pending group supplied inference or direct fallback", err)
			}
			if client, release, err := r.AcquireQuotaManagement("a"); err == nil || client != nil || release != nil {
				t.Fatal("pending group supplied management lease")
			}
			r.ReconcileAuto(accounts, time.Now())
			if capacity() != 0 || r.state.IPGroups[ip].Owner != "" || len(r.state.IPGroups[ip].AccountIDs) != 1 {
				t.Fatal("pending group reassigned or increased N")
			}
			accounts["b"] = kilo.EgressAccountStatus{Provider: "kilo", CanAllocate: true}
			specified := kilo.CapacityPolicy{CapacityMode: "ip", AllocationMode: "specified", MaxIPs: 1, SpecifiedIPs: []string{ip}}
			if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, EgressAccount: "b", Mode: "auto", CapacityPolicy: specified}, accounts); !errors.Is(err, kilo.ErrStateConflict) || len(r.state.IPGroups[ip].AccountIDs) != 1 {
				t.Fatal("manual specified assignment consumed pending group", err)
			}
			t.Log("IP capacity 20 to 0; pending alias blocks auto/specified and leases; new duplicate leaves trusted group and N unchanged")
		})
	}
}

func TestIPAdmissionObservationCommitCurrentIdentity(t *testing.T) {
	for _, action := range []string{"reverify", "change_url", "unrelated_revision"} {
		t.Run(action, func(t *testing.T) {
			r := admissionRuntime(t)
			p := admissionFinish(t, r, admissionCreate(t, r, "http://127.0.0.1:32101"), "203.0.113.10")
			r.markFailed(p.ID)
			generation := r.health[p.ID].generation
			if r.StateView().Proxies[0].Healthy {
				t.Fatal("observation not fenced before commit")
			}
			switch action {
			case "change_url":
				url := "http://127.0.0.1:32102"
				if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: p.ID, URL: &url}, nil); err != nil {
					t.Fatal(err)
				}
				admissionFinish(t, r, p, "203.0.113.10")
			case "reverify":
				admissionFinish(t, r, p, "203.0.113.10")
			case "unrelated_revision":
				name := "renamed"
				if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: p.ID, Name: &name}, nil); err != nil {
					t.Fatal(err)
				}
			}
			revision := r.state.Revision
			if err := r.CommitProxyObservation(p.ID, p.URL, generation); err != nil {
				t.Fatal(err)
			}
			view := r.StateView().Proxies[0]
			if action == "unrelated_revision" {
				if view.AdmissionStatus != "pending" || view.AdmissionReason != "probe_failed" || view.Healthy || r.state.Revision != revision+1 {
					t.Fatal("unrelated revision lost observed quarantine", view)
				}
			} else if view.AdmissionStatus != "admitted" || !view.Healthy || r.state.Revision != revision {
				t.Fatal("late observation overwrote newer verification/URL", view)
			}
		})
	}
}
