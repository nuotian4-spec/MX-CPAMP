package kilo

import (
	"errors"
	"reflect"
	"testing"
)

func TestUX1CandidateScopeAndDirectCapacity(t *testing.T) {
	proxies := []Proxy{{ID: "selected", Healthy: true}, {ID: "unselected", Healthy: true}}
	p := newTestPool(t, proxies, 1)
	p.Reconfigure(proxies, 1, map[string]AccountEgress{"a": {Mode: "pool", CandidateProxyIDs: []string{"selected"}}}, nil)
	first := acquireTest(t, p, "a", AcquireOptions{})
	if first.Route().ProxyID != "selected" {
		t.Fatal(first.Route())
	}
	if _, err := p.TryAcquire("a", AcquireOptions{}); !errors.Is(err, ErrProxyCapacity) {
		t.Fatal(err)
	}
	direct := acquireTest(t, p, "a", AcquireOptions{AllowDirectWhenProxyFull: true})
	if direct.Route().Mode != RouteDirect {
		t.Fatal(direct.Route())
	}
	first.Release()
	_ = p.SetHealthy("selected", false)
	if _, err := p.TryAcquire("a", AcquireOptions{AllowDirectWhenProxyFull: true}); !errors.Is(err, ErrProxyUnavailable) {
		t.Fatal(err)
	}
	legacy := acquireTest(t, p, "legacy", AcquireOptions{})
	if legacy.Route().ProxyID != "unselected" {
		t.Fatal(legacy.Route())
	}
}
func TestUX1CandidatePersistenceAndValidation(t *testing.T) {
	s := ProxyState{Revision: 1, Limit: 2, Proxies: []SavedProxy{{ID: "p1", URL: "http://127.0.0.1:1"}, {ID: "p2", URL: "http://127.0.0.1:2"}}, Accounts: map[string]AccountEgress{"a": {Mode: "pool", CandidateProxyIDs: []string{"p2"}, PrimaryProxyID: "p2"}, "legacy": {Mode: "pool"}}}
	store := ProxyStateStore{Dir: t.TempDir()}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	got, _, err := store.Load(ProxyState{})
	if err != nil || !reflect.DeepEqual(got.Accounts, s.Accounts) {
		t.Fatalf("%+v %v", got.Accounts, err)
	}
	clone := s.Clone()
	a := clone.Accounts["a"]
	a.CandidateProxyIDs[0] = "p1"
	clone.Accounts["a"] = a
	if s.Accounts["a"].CandidateProxyIDs[0] != "p2" || clone.Validate() == nil {
		t.Fatal("clone aliases or primary outside set accepted")
	}
	for _, ids := range [][]string{{}, {"missing"}, {"p2", "p2"}} {
		bad := s.Clone()
		a := bad.Accounts["a"]
		a.CandidateProxyIDs = ids
		bad.Accounts["a"] = a
		if bad.Validate() == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
}
