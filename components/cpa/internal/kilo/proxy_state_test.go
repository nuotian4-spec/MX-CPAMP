package kilo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedExtraPromotionAndFirstUsedOrder(t *testing.T) {
	proxies := []Proxy{{ID: "p0", Healthy: true, FirstUsed: 1}, {ID: "p2", Healthy: true}, {ID: "p1", Healthy: true, FirstUsed: 2}}
	p := newTestPool(t, proxies, 1)
	var promoted []string
	fail := false
	p.Reconfigure(proxies, 1, map[string]AccountEgress{"a": {Mode: "pool", PrimaryProxyID: "p0"}}, func(a, id string, primary bool) (uint64, error) {
		if fail {
			return 0, ErrStateStorage
		}
		if primary {
			promoted = append(promoted, id)
		}
		if id == "p0" {
			return 1, nil
		}
		if id == "p1" {
			return 2, nil
		}
		return 3, nil
	})
	a := acquireTest(t, p, "a", AcquireOptions{})
	b := acquireTest(t, p, "a", AcquireOptions{})
	if a.Route().ProxyID != "p0" || b.Route().ProxyID != "p1" {
		t.Fatalf("first-used order: %s %s", a.Route().ProxyID, b.Route().ProxyID)
	}
	_ = p.SetHealthy("p0", false)
	fail = true
	if lease, err := p.TryAcquire("a", AcquireOptions{}); !errors.Is(err, ErrStateStorage) || lease != nil {
		t.Fatalf("promotion save failure=%v %v", lease, err)
	}
	if p.primary["a"] != "p0" || len(p.extras["a"]) != 1 {
		t.Fatal("failed save published primary/extras")
	}
	fail = false
	c := acquireTest(t, p, "a", AcquireOptions{})
	if p.primary["a"] != "p1" || c.Route().ProxyID != "p2" || len(promoted) != 1 || promoted[0] != "p1" {
		t.Fatalf("saturated healthy extra not promoted: primary=%s route=%+v promoted=%v", p.primary["a"], c.Route(), promoted)
	}
	b.Release()
	c.Release()
	a.Release()
	if len(p.extras["a"]) != 0 {
		t.Fatal("last release retained extras")
	}
	other := acquireTest(t, p, "b", AcquireOptions{})
	if other.Route().ProxyID != "p1" {
		t.Fatalf("shared public reuse=%s", other.Route().ProxyID)
	}
}
func TestProxyStateRoundtripAndMalformedFailClosed(t *testing.T) {
	store := ProxyStateStore{Dir: t.TempDir()}
	s := ProxyState{Revision: 1, Limit: 3, Proxies: []SavedProxy{{ID: "stable", URL: "http://user:pass@localhost:7890", Name: "named", AccountID: "a", Order: 7, FirstUsed: 2}}, Accounts: map[string]AccountEgress{"a": {Mode: "pool", PrimaryProxyID: "stable"}, "b": {Mode: "direct"}}}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	got, managed, err := store.Load(ProxyState{Limit: 99})
	if err != nil || !managed || got.Revision != 1 || got.Limit != 3 || got.Proxies[0] != s.Proxies[0] || got.Accounts["b"].Mode != "direct" {
		t.Fatalf("roundtrip=%+v managed=%t err=%v", got, managed, err)
	}
	for _, raw := range []string{`{}`, `{"revision":1,"proxy_limit":3,"proxies":[],"accounts":{"a":{"mode":"unknown","primary_proxy_id":""}}}`, `{"revision":1,"proxy_limit":3,"proxies":[{"id":"p","url":"direct","enabled":true,"order":0,"first_used":0,"name":"","account_id":""}],"accounts":{}}`} {
		if err = os.WriteFile(filepath.Join(store.Dir, StateFileName), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err = store.Load(s); !errors.Is(err, ErrStateStorage) {
			t.Fatalf("bad state accepted=%v", err)
		}
	}
}
