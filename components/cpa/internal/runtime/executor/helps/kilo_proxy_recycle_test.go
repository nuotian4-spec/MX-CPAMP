package helps

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

func TestM3BArchivedProbeGenerationCannotOverwriteRestore(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "CONNECT" || req.Host != "api.ipify.org:443" {
			t.Errorf("unexpected probe target=%s %s", req.Method, req.Host)
		}
		close(started)
		select {
		case <-finish:
			http.Error(w, "late failure", 502)
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(proxy.Close)
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2, Proxies: []config.KiloProxy{{ID: "p", URL: proxy.URL}}}})
	if err != nil {
		t.Fatal(err)
	}
	r.probeSequence = 1
	r.health["p"] = kiloProxyHealth{pending: true, generation: 1}
	done := make(chan struct{})
	go func() { r.probe("p", proxy.URL, 1); close(done) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not enter CONNECT")
	}
	if _, err = r.ChangeState(kilo.ProxyChanges{ExpectedRevision: 0, ID: "p", Archive: true}, nil); err != nil {
		t.Fatal(err)
	}
	p, err := r.ProxyForProbe(1, "p", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CompleteProxyProbe(1, p, true, nil); err != nil {
		t.Fatal(err)
	}
	close(finish)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("old probe did not finish")
	}
	view := r.StateView()
	if view.Revision != 2 || !view.Proxies[0].Healthy || len(view.Recycle) != 0 || r.health["p"].generation != 2 {
		t.Fatalf("old same-URL probe overwrote restored health: %+v", view)
	}
}

func TestM3BFirstUsedOrderAcrossArchiveAllocationRestore(t *testing.T) {
	dir := t.TempDir()
	state := kilo.ProxyState{Revision: 1, Limit: 2,
		Proxies:  []kilo.SavedProxy{{ID: "old", URL: "http://127.0.0.1:1234", FirstUsed: 9, Order: 1}, {ID: "new", URL: "http://127.0.0.1:1234"}},
		Accounts: map[string]kilo.AccountEgress{}}
	store := kilo.ProxyStateStore{Dir: dir}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ChangeState(kilo.ProxyChanges{ExpectedRevision: 1, ID: "old", Archive: true}, nil); err != nil {
		t.Fatal(err)
	}
	p, err := r.ProxyForProbe(2, "new", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CompleteProxyProbe(2, p, false, nil); err != nil {
		t.Fatal(err)
	}
	first, err := r.pool.TryAcquire("account-a", kilo.AcquireOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first.Release()
	if r.state.Proxies[0].FirstUsed != 10 {
		t.Fatalf("new allocation reused archived ordinal: %+v", r.state.Proxies)
	}
	p, err = r.ProxyForProbe(3, "old", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CompleteProxyProbe(3, p, true, nil); err != nil {
		t.Fatal(err)
	}
	second, err := r.pool.TryAcquire("account-b", kilo.AcquireOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if second.Route().ProxyID != "old" {
		t.Fatalf("newer proxy preceded restored older proxy: %+v", second.Route())
	}
	saved, _, err := store.Load(kilo.ProxyState{})
	if err != nil || saved.Proxies[0].FirstUsed != 10 || saved.Proxies[1].FirstUsed != 9 {
		t.Fatalf("first-use chronology not persisted: %+v %v", saved, err)
	}
}
