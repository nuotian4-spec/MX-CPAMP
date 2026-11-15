package executor

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
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type ipAdmissionWire struct {
	mu      sync.Mutex
	ip      string
	entered chan struct{}
	proceed <-chan struct{}
	fail    bool
	proxy   *httptest.Server
}

func newIPAdmissionWire(t *testing.T) *ipAdmissionWire {
	t.Helper()
	f := &ipAdmissionWire{ip: "203.0.113.42"}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Host != "api.ipify.org" {
			t.Errorf("unexpected synthetic request %s %s", r.Method, r.Host)
			http.Error(w, "unexpected", 400)
			return
		}
		f.mu.Lock()
		ip, entered, proceed, fail := f.ip, f.entered, f.proceed, f.fail
		f.mu.Unlock()
		if entered != nil {
			entered <- struct{}{}
		}
		if proceed != nil {
			select {
			case <-proceed:
			case <-r.Context().Done():
				return
			}
		}
		if fail {
			c, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = c.Close()
			}
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
func ipAdmissionExecutor(t *testing.T) *KiloExecutor {
	t.Helper()
	e := NewKiloExecutor(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2}})
	if e.initErr != nil {
		t.Fatal(e.initErr)
	}
	return e
}
func ipAdmissionCreate(t *testing.T, e *KiloExecutor, url string) string {
	t.Helper()
	v, err := e.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	v, err = e.ChangeKiloProxyState(kilo.ProxyChanges{ExpectedRevision: v.Revision, Create: true, URL: &url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return v.Proxies[len(v.Proxies)-1].ID
}
func ipAdmissionProbe(t *testing.T, e *KiloExecutor, id string) {
	t.Helper()
	v, _ := e.KiloProxyState()
	if _, err := e.ProbeKiloProxy(context.Background(), v.Revision, id, false); err != nil {
		t.Fatal(err)
	}
}
func ipAdmissionWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("synthetic probe did not progress")
	}
}

func TestIPAdmissionExecutorConcurrentSameIPAndRevision(t *testing.T) {
	f := newIPAdmissionWire(t)
	e := ipAdmissionExecutor(t)
	ids := []string{ipAdmissionCreate(t, e, f.proxy.URL), ipAdmissionCreate(t, e, f.proxy.URL)}
	v, _ := e.KiloProxyState()
	entered := make(chan struct{}, 2)
	proceed := make(chan struct{})
	f.mu.Lock()
	f.entered, f.proceed = entered, proceed
	f.mu.Unlock()
	results := make(chan error, 2)
	for _, id := range ids {
		go func(id string) {
			_, err := e.ProbeKiloProxy(context.Background(), v.Revision, id, false)
			results <- err
		}(id)
	}
	ipAdmissionWait(t, entered)
	ipAdmissionWait(t, entered)
	close(proceed)
	successes := 0
	for range ids {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, kilo.ErrStateConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d duplicates", successes)
	}
	f.mu.Lock()
	f.entered, f.proceed = nil, nil
	f.mu.Unlock()
	v, _ = e.KiloProxyState()
	pending := ""
	for _, p := range v.Proxies {
		if p.AdmissionStatus == "pending" {
			pending = p.ID
		}
	}
	if _, err := e.ProbeKiloProxy(context.Background(), v.Revision, pending, false); !errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
		t.Fatal("recheck did not report duplicate", err)
	}
	state, _, err := (kilo.ProxyStateStore{Dir: e.runtime.StateDirectory()}).Load(kilo.ProxyState{})
	if err != nil || len(state.IPHistory) != 1 || state.Revision != v.Revision {
		t.Fatal("duplicate mutated persistent state", err)
	}
}
func TestIPAdmissionExecutorCancelledAndConcurrentEdit(t *testing.T) {
	f := newIPAdmissionWire(t)
	e := ipAdmissionExecutor(t)
	id := ipAdmissionCreate(t, e, f.proxy.URL)
	v, _ := e.KiloProxyState()
	entered := make(chan struct{}, 1)
	proceed := make(chan struct{})
	f.mu.Lock()
	f.entered, f.proceed = entered, proceed
	f.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := e.ProbeKiloProxy(context.Background(), v.Revision, id, false); done <- err }()
	ipAdmissionWait(t, entered)
	name := "edited during observation"
	if _, err := e.ChangeKiloProxyState(kilo.ProxyChanges{ExpectedRevision: v.Revision, ID: id, Name: &name}, nil); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if err := <-done; !errors.Is(err, kilo.ErrStateConflict) {
		t.Fatal(err)
	}
	v, _ = e.KiloProxyState()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.ProbeKiloProxy(ctx, v.Revision, id, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	state, _, err := (kilo.ProxyStateStore{Dir: e.runtime.StateDirectory()}).Load(kilo.ProxyState{})
	if err != nil || len(state.IPHistory) != 0 || state.Proxies[0].Name != name {
		t.Fatal("stale probe changed admission", err)
	}
}
func TestIPAdmissionExecutorBackgroundFailureSerializesAndRejectsOldRuntime(t *testing.T) {
	f := newIPAdmissionWire(t)
	e := ipAdmissionExecutor(t)
	id := ipAdmissionCreate(t, e, f.proxy.URL)
	ipAdmissionProbe(t, e, id)
	admitted, release, wait, err := e.TryAcquireEgress(&coreauth.Auth{ID: "a", Provider: "kilo"})
	if err != nil || wait {
		t.Fatal(err)
	}
	defer release()
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	e.mu.Lock()
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest("GET", "https://api.ipify.org/", nil)
		_, err := helps.KiloDo(context.Background(), admitted, req)
		done <- err
	}()
	change := e.runtime.Changes()
	select {
	case <-change:
	case <-time.After(5 * time.Second):
		e.mu.Unlock()
		t.Fatal("failure did not quarantine before state lock")
	}
	view := e.runtime.StateView()
	name := "concurrent management"
	if _, err = e.runtime.ChangeState(kilo.ProxyChanges{ExpectedRevision: view.Revision, ID: id, Name: &name}, nil); err != nil {
		e.mu.Unlock()
		t.Fatal(err)
	}
	e.mu.Unlock()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("synthetic failure accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("state callback deadlocked")
	}
	view, _ = e.KiloProxyState()
	if view.Proxies[0].AdmissionStatus != "pending" || view.Proxies[0].Name != name || view.Proxies[0].Healthy {
		t.Fatal(view)
	}
	reloaded := NewKiloExecutor(&config.Config{Kilo: config.KiloConfig{StateDir: e.runtime.StateDirectory()}})
	v, _ := reloaded.KiloProxyState()
	if v.Proxies[0].AdmissionStatus != "pending" {
		t.Fatal("failure was not persisted")
	}
	old := e.runtime
	callback := e.observationCommit(old)
	fresh, err := helps.NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.runtime = fresh
	e.mu.Unlock()
	callback(id, f.proxy.URL, 1)
	v, _ = e.KiloProxyState()
	if len(v.Proxies) != 0 || v.Revision != 0 {
		t.Fatal("old runtime callback changed replacement")
	}
}
func TestIPAdmissionExecutorBackgroundSaveFailureKeepsMemoryFence(t *testing.T) {
	f := newIPAdmissionWire(t)
	e := ipAdmissionExecutor(t)
	id := ipAdmissionCreate(t, e, f.proxy.URL)
	ipAdmissionProbe(t, e, id)
	admitted, release, wait, err := e.TryAcquireEgress(&coreauth.Auth{ID: "a", Provider: "kilo"})
	if err != nil || wait {
		t.Fatal(err)
	}
	defer release()
	path := filepath.Join(e.runtime.StateDirectory(), kilo.StateFileName)
	backup, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path); _ = os.WriteFile(path, backup, 0600) })
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	req, _ := http.NewRequest("GET", "https://api.ipify.org/", nil)
	if _, err = helps.KiloDo(context.Background(), admitted, req); err == nil {
		t.Fatal("synthetic failure accepted")
	}
	v, _ := e.KiloProxyState()
	if v.Proxies[0].Healthy || v.AutoError == nil || *v.AutoError != "storage_failed" || v.Proxies[0].AdmissionStatus != "admitted" {
		t.Fatal("save failure misreported persistence", v)
	}
	if a, rel, _, _ := e.TryAcquireEgress(&coreauth.Auth{ID: "b", Provider: "kilo"}); a != nil || rel != nil {
		t.Fatal("failed persistence re-enabled resources")
	}
}
