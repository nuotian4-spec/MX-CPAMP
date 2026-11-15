package cliproxy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	i3MachineOne    = "11111111-1111-4111-8111-111111111111"
	i3MachineTwo    = "22222222-2222-4222-8222-222222222222"
	i3MachineIdle   = "33333333-3333-4333-8333-333333333333"
	i3MachineLegacy = "44444444-4444-4444-8444-444444444444"
	i3MachineOther  = "55555555-5555-4555-8555-555555555555"
	i3ProxySecret   = "identity-proxy-password"
)

type i3CountingStore struct {
	coreauth.Store
	saves atomic.Int32
}

func (s *i3CountingStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	s.saves.Add(1)
	return s.Store.Save(ctx, a)
}

func i3Profiles(machines ...string) []any {
	items := make([]any, 0, len(machines))
	for _, machine := range machines {
		items = append(items, map[string]any{"id": machine, "machine_id": machine})
	}
	return items
}

func i3Register(t *testing.T, f *kiloNativeFixture, id string, identity map[string]any) {
	t.Helper()
	metadata := map[string]any{"type": "kilo", "api_key": kiloInitialKey, "access_token": "i3-synthetic-access-token", "proxy_auth": i3ProxySecret}
	for key, value := range identity {
		metadata[key] = value
	}
	_, err := f.manager.Register(context.Background(), &coreauth.Auth{ID: id, FileName: id, Provider: "kilo", Label: id, Status: coreauth.StatusActive, Attributes: map[string]string{"priority": "7"}, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
}

func i3ReadSnapshot(t *testing.T, f *kiloNativeFixture) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, a := range f.manager.List() {
		doc := ojson.NewDocString("{}")
		if err := doc.Set("metadata", a.Metadata); err != nil {
			t.Fatal(err)
		}
		out["metadata:"+a.ID] = doc.String()
		raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, a.ID))
		if err != nil {
			t.Fatal(err)
		}
		out["file:"+a.ID] = string(raw)
	}
	return out
}

func i3Accounts(t *testing.T, raw []byte) map[string]ojson.Result {
	t.Helper()
	for _, secret := range []string{i3MachineOne, i3MachineTwo, i3MachineIdle, i3MachineLegacy, i3MachineOther, kiloInitialKey, "i3-synthetic-access-token", i3ProxySecret} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("identity response leaked a synthetic secret")
		}
	}
	items := ojson.GetBytes(raw, "accounts")
	if !items.IsArray() {
		t.Fatal("accounts is not an array")
	}
	out := map[string]ojson.Result{}
	for _, item := range items.Array() {
		out[item.Get("id").String()] = item
	}
	return out
}

func TestI3ClientIdentityReadOnlyStatesWithoutExecutor(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	i3Register(t, f, "i3-persisted.json", map[string]any{coreauth.KiloMachineIDKey: i3MachineOne, coreauth.KiloDeviceProfilesKey: i3Profiles(i3MachineOne, i3MachineTwo)})
	i3Register(t, f, "i3-legacy.json", map[string]any{coreauth.KiloMachineIDKey: i3MachineLegacy})
	i3Register(t, f, "i3-pending.json", nil)
	i3Register(t, f, "i3-invalid.json", map[string]any{coreauth.KiloMachineIDKey: 42})
	before := i3ReadSnapshot(t, f)
	store := &i3CountingStore{Store: m2FileStore(f)}
	f.manager.SetStore(store)
	for range 2 {
		accounts := i3Accounts(t, m2Request(t, f, "GET", kiloAccountsURL, "", 200))
		if len(accounts) != 4 {
			t.Fatalf("account count=%d", len(accounts))
		}
		for _, tc := range []struct {
			id, state string
			refs      int
		}{
			{"i3-persisted.json", "persisted", 2}, {"i3-legacy.json", "persisted", 1},
			{"i3-pending.json", "pending", 0}, {"i3-invalid.json", "invalid", 0},
		} {
			a := accounts[tc.id]
			identity := a.Get("client_identity")
			profiles := identity.Get("profiles")
			if identity.Get("state").String() != tc.state || !profiles.IsArray() || len(profiles.Array()) != tc.refs {
				t.Fatalf("%s identity=%s", tc.id, identity.Raw)
			}
			for i, p := range profiles.Array() {
				if p.Get("ref").String() != []string{"profile-1", "profile-2"}[i] || p.Get("exits").Exists() {
					t.Fatalf("no-executor profile=%s", p.Raw)
				}
			}
			if a.Get("name").String() != tc.id || !a.Get("enabled").Bool() || a.Get("priority").Int() != 7 || !a.Get("effective_limit").Exists() || !a.Get("quota_snapshot").Exists() {
				t.Fatalf("existing account fields changed: %s", a.Raw)
			}
		}
	}
	if _, exists := f.manager.Executor("kilo"); exists {
		t.Fatal("GET created an executor")
	}
	if store.saves.Load() != 0 || f.posts.Load() != 0 || f.probes.Load() != 0 || !reflect.DeepEqual(before, i3ReadSnapshot(t, f)) {
		t.Fatalf("GET changed read-only state: save=%d model=%d probe=%d", store.saves.Load(), f.posts.Load(), f.probes.Load())
	}
	t.Log("I3 no-executor GET: persisted pair, legacy, pending, invalid; secrets absent; Save=0 model=0 probe=0; identities/files unchanged")
}

func i3IdentityFixture(t *testing.T) (*kiloNativeFixture, *i3CountingStore) {
	t.Helper()
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	i3Register(t, f, "i3-persisted.json", map[string]any{coreauth.KiloMachineIDKey: i3MachineOne, coreauth.KiloDeviceProfilesKey: i3Profiles(i3MachineOne, i3MachineTwo, i3MachineIdle)})
	i3Register(t, f, "i3-legacy.json", map[string]any{coreauth.KiloMachineIDKey: i3MachineLegacy})
	i3Register(t, f, "i3-pending.json", nil)
	i3Register(t, f, "i3-invalid.json", map[string]any{coreauth.KiloMachineIDKey: 42})
	i3Register(t, f, "i3-stale.json", map[string]any{coreauth.KiloMachineIDKey: i3MachineOne, coreauth.KiloDeviceProfilesKey: i3Profiles(i3MachineOne, i3MachineTwo)})
	i3Register(t, f, "i3-unbound.json", map[string]any{coreauth.KiloMachineIDKey: i3MachineIdle})
	state := kilo.ProxyState{
		Revision: 1, Limit: 2, IPConcurrencyLimit: 2, MaxUniqueAccounts: 1,
		Proxies: []kilo.SavedProxy{
			{ID: "i3-alias-one", URL: "http://user:" + i3ProxySecret + "@127.0.0.1:1", Name: "first alias", FixedIP: "8.8.8.8"},
			{ID: "i3-alias-two", URL: "http://user:" + i3ProxySecret + "@127.0.0.1:2", Name: "second alias", Order: 1, FixedIP: "8.8.8.8"},
		},
		IPGroups: map[string]kilo.IPGroup{"8.8.8.8": {IP: "8.8.8.8", Owner: "i3-persisted.json", AccountIDs: []string{"i3-persisted.json"}}},
		Accounts: map[string]kilo.AccountEgress{
			"i3-persisted.json": {Mode: "pool", CandidateProxyIDs: []string{"i3-alias-one", "i3-alias-two"}, DeviceProfiles: map[string]string{"8.8.8.8": i3MachineOne, "direct": i3MachineTwo, "http://user:" + i3ProxySecret + "@127.0.0.1:1": i3MachineOne}},
			"i3-legacy.json":    {Mode: "pool", DeviceProfiles: map[string]string{"legacy": i3MachineLegacy}},
			"i3-stale.json":     {Mode: "pool", DeviceProfiles: map[string]string{"8.8.4.4": i3MachineOne, "1.1.1.1": i3MachineOther}},
			"i3-unbound.json":   {Mode: "pool"},
		},
	}
	if err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Save(state); err != nil {
		t.Fatal(err)
	}
	f.executor = native.NewKiloExecutor(f.cfg)
	f.manager.RegisterExecutor(f.executor)
	store := &i3CountingStore{Store: m2FileStore(f)}
	f.manager.SetStore(store)
	return f, store
}

func i3StateBytes(t *testing.T, f *kiloNativeFixture) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestI3ClientIdentityReadOnlyEgressRelations(t *testing.T) {
	f, store := i3IdentityFixture(t)
	before := i3ReadSnapshot(t, f)
	stateBefore := i3StateBytes(t, f)
	viewBefore, err := f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		accounts := i3Accounts(t, m2Request(t, f, "GET", kiloAccountsURL, "", 200))
		if len(accounts) != 6 {
			t.Fatalf("account count=%d", len(accounts))
		}
		profiles := accounts["i3-persisted.json"].Get("client_identity.profiles").Array()
		if len(profiles) != 3 || profiles[0].Get("ref").String() != "profile-1" || profiles[1].Get("ref").String() != "profile-2" || profiles[2].Get("ref").String() != "profile-3" {
			t.Fatal("persisted profile order changed")
		}
		if exits := profiles[0].Get("exits"); !exits.IsArray() || len(exits.Array()) != 1 || exits.Get("0.kind").String() != "ip" || exits.Get("0.ip").String() != "8.8.8.8" {
			t.Fatalf("same-IP identity mapping=%s", exits.Raw)
		}
		if exits := profiles[1].Get("exits"); !exits.IsArray() || len(exits.Array()) != 1 || exits.Get("0.kind").String() != "direct" || exits.Get("0.ip").Exists() {
			t.Fatalf("direct identity mapping=%s", exits.Raw)
		}
		if exits := profiles[2].Get("exits"); !exits.IsArray() || len(exits.Array()) != 0 {
			t.Fatalf("idle profile association=%s", exits.Raw)
		}
		legacy := accounts["i3-legacy.json"].Get("client_identity.profiles.0.exits")
		if !legacy.IsArray() || len(legacy.Array()) != 1 || legacy.Get("0.kind").String() != "legacy" || legacy.Get("0.ip").Exists() {
			t.Fatalf("legacy identity mapping=%s", legacy.Raw)
		}
		unbound := accounts["i3-unbound.json"].Get("client_identity.profiles.0.exits")
		if !unbound.IsArray() || len(unbound.Array()) != 0 {
			t.Fatalf("known empty relationship=%s", unbound.Raw)
		}
		stale := accounts["i3-stale.json"].Get("client_identity")
		if stale.Get("state").String() != "persisted" || len(stale.Get("profiles").Array()) != 2 {
			t.Fatalf("snapshot skew changed identity state=%s", stale.Raw)
		}
		for _, profile := range stale.Get("profiles").Array() {
			if profile.Get("exits").Exists() {
				t.Fatal("unmatched profile ID exposed partial associations")
			}
		}
	}
	bindingsSource, ok := any(f.executor).(interface {
		KiloDeviceProfileBindings() (map[string]map[string]string, error)
	})
	if !ok {
		t.Fatal("read-only relationship snapshot missing")
	}
	bindings, err := bindingsSource.KiloDeviceProfileBindings()
	if err != nil {
		t.Fatal(err)
	}
	bindings["i3-persisted.json"]["8.8.8.8"] = "changed-copy"
	delete(bindings, "i3-legacy.json")
	again, err := bindingsSource.KiloDeviceProfileBindings()
	if err != nil || again["i3-persisted.json"]["8.8.8.8"] != i3MachineOne || again["i3-legacy.json"]["legacy"] != i3MachineLegacy {
		t.Fatal("relationship view aliases live state")
	}
	viewAfter, err := f.executor.KiloProxyState()
	if err != nil || !reflect.DeepEqual(viewBefore, viewAfter) || !bytes.Equal(stateBefore, i3StateBytes(t, f)) || !reflect.DeepEqual(before, i3ReadSnapshot(t, f)) {
		t.Fatal("identity GET changed credential or egress state/capacity")
	}
	if store.saves.Load() != 0 || f.posts.Load() != 0 || f.probes.Load() != 0 || f.connects.Load() != 0 {
		t.Fatalf("GET side effects: save=%d model=%d probe=%d connect=%d", store.saves.Load(), f.posts.Load(), f.probes.Load(), f.connects.Load())
	}
	t.Log("I3 egress GET: same-IP aliases share profile-1; direct profile-2; idle profile-3; legacy, empty, skew preserved; Save/model/probe/connect=0; credential and relation bytes/capacity unchanged")
}

func TestI3ClientIdentityUnavailableRelationSource(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	i3Register(t, f, "i3-source-error.json", map[string]any{coreauth.KiloMachineIDKey: i3MachineOne})
	// A real executor without an initialized runtime reports a relation read error.
	f.manager.RegisterExecutor(&native.KiloExecutor{})
	store := &i3CountingStore{Store: m2FileStore(f)}
	f.manager.SetStore(store)
	before := i3ReadSnapshot(t, f)
	for range 2 {
		accounts := i3Accounts(t, m2Request(t, f, "GET", kiloAccountsURL, "", 200))
		identity := accounts["i3-source-error.json"].Get("client_identity")
		if identity.Get("state").String() != "persisted" || identity.Get("profiles.0.ref").String() != "profile-1" || identity.Get("profiles.0.exits").Exists() {
			t.Fatalf("relation source error changed identity=%s", identity.Raw)
		}
	}
	if store.saves.Load() != 0 || f.posts.Load() != 0 || f.probes.Load() != 0 || !reflect.DeepEqual(before, i3ReadSnapshot(t, f)) {
		t.Fatal("unavailable relationship read changed persisted identity")
	}
	t.Log("I3 unavailable executor relation source: persisted identity retained, exits omitted, Save/model/probe=0, file/metadata unchanged")
}
