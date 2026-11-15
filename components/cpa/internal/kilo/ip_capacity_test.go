package kilo

import (
	"github.com/MoeForge/moego/ojson"
	"os"
	"path/filepath"
	"testing"
)

func TestUX2LegacySingleIPStateDefaults(t *testing.T) {
	store := ProxyStateStore{Dir: t.TempDir()}
	raw := `{"revision":1,"proxy_limit":10,"auto_assignment_enabled":true,"max_unique_accounts":3,"proxies":[{"id":"p","url":"http://127.0.0.1:32101","name":"","account_id":"","order":0,"enabled":true,"first_used":1,"fixed_exit_ip":"203.0.113.1"}],"accounts":{"a":{"mode":"auto","primary_proxy_id":"p","candidate_proxy_ids":null}},"ip_groups":{"203.0.113.1":{"owner_account_id":"a","account_ids":["a"],"draining":false,"retired":false,"reason":""}}}`
	if err := os.WriteFile(filepath.Join(store.Dir, StateFileName), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	state, _, err := store.Load(ProxyState{})
	if err != nil {
		t.Fatal(err)
	}
	a := state.Accounts["a"].Normalized()
	if a.CapacityMode != "fixed" || a.MaxIPs != 1 || state.IPCapacity() != 20 || state.IPEntryLimit != nil || len(state.OwnedIPs("a")) != 1 {
		t.Fatal("legacy interpretation changed")
	}
	if err = store.Save(state); err != nil {
		t.Fatal(err)
	}
	again, _, err := store.Load(ProxyState{})
	if err != nil || again.Accounts["a"].CapacityMode != "fixed" {
		t.Fatal("legacy rewrite/reload")
	}
	for _, body := range []string{`{"mode":"auto","capacity_mode":"ip","max_ips":0}`, `{"mode":"auto","capacity_mode":"ip","allocation_mode":"specified","specified_ips":[]}`, `{"mode":"direct","capacity_mode":"ip"}`} {
		if _, err := DecodeCapacityPolicy(ojson.Parse(body)); err == nil {
			t.Fatalf("invalid policy accepted: %s", body)
		}
	}
	t.Log("legacy single-IP JSON loads fixed/default20, rewrites and reloads without changing owner or max1")
}
