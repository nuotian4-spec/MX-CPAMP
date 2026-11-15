package kilo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIPAdmissionStateRoundtripCloneAndValidation(t *testing.T) {
	at := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	p := SavedProxy{ID: "p", URL: "http://127.0.0.1:32101", AdmissionStatus: "admitted", AdmittedIP: "203.0.113.1", AdmittedAt: at}
	s := ProxyState{Revision: 1, Limit: 2, Proxies: []SavedProxy{p}, Accounts: map[string]AccountEgress{}, IPHistory: map[string]ProxyIPRegistration{p.AdmittedIP: {IP: p.AdmittedIP, ProxyIDs: []string{p.ID}, FirstAdmittedAt: &at, LastObservedAt: &at, Source: "admission"}}}
	store := ProxyStateStore{Dir: t.TempDir()}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	got, _, err := store.Load(ProxyState{})
	if err != nil || got.Proxies[0] != p || len(got.IPHistory) != 1 {
		t.Fatalf("roundtrip: %+v %v", got, err)
	}
	clone := got.Clone()
	entry := clone.IPHistory[p.AdmittedIP]
	entry.ProxyIDs[0] = "changed"
	*entry.FirstAdmittedAt = at.Add(time.Hour)
	if got.IPHistory[p.AdmittedIP].ProxyIDs[0] != "p" || !got.IPHistory[p.AdmittedIP].FirstAdmittedAt.Equal(at) {
		t.Fatal("clone mutated history")
	}
	bad := got.Clone()
	bad.Proxies[0].AdmittedIP = "203.0.113.2"
	if bad.Validate() == nil {
		t.Fatal("unregistered admitted IP accepted")
	}
	bad = got.Clone()
	bad.Proxies[0].AdmissionStatus = "unknown"
	if bad.Validate() == nil {
		t.Fatal("invalid status accepted")
	}
	raw := `{"revision":1,"proxy_limit":2,"accounts":{},"proxies":[{"id":"old","url":"http://127.0.0.1:1","name":"","account_id":"","order":0,"enabled":true,"first_used":0}]}`
	if err = os.WriteFile(filepath.Join(store.Dir, StateFileName), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	got, _, err = store.Load(ProxyState{})
	if err != nil || got.Proxies[0].AdmissionState() != "legacy" || len(got.IPHistory) != 0 {
		t.Fatalf("legacy synthesized history: %+v %v", got, err)
	}
}
