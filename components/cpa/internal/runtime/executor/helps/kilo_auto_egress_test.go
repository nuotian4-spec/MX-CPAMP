package helps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestAE1ExplicitManualExitWaitsForResourcesAndPreservesHistory(t *testing.T) {
	for _, limit := range []int{1, 3} {
		r, accounts := ae1AutoRuntime(t, limit)
		r.ReconcileAuto(accounts, time.Now())
		_, release, wait, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
		if err != nil || wait {
			t.Fatal("lease absent")
		}
		change := kilo.ProxyChanges{ExpectedRevision: r.state.Revision, EgressAccount: "a", Mode: "direct"}
		if _, err = r.ChangeState(change, accounts); !errors.Is(err, kilo.ErrStateBusy) {
			t.Fatalf("active manual exit=%v", err)
		}
		release()
		original := r.store.Dir
		bad := filepath.Join(t.TempDir(), "file")
		if err = os.WriteFile(bad, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		r.store.Dir = bad
		if _, err = r.ChangeState(change, accounts); err == nil || r.state.IPGroups[ae1IP].Owner != "a" {
			t.Fatal("failed exit changed owner")
		}
		r.store.Dir = original
		if _, err = r.ChangeState(change, accounts); err != nil {
			t.Fatal(err)
		}
		g := r.state.IPGroups[ae1IP]
		if g.Owner != "" || len(g.AccountIDs) != 1 || g.Retired != (limit == 1) || r.state.Accounts["a"].Mode != "direct" {
			t.Fatalf("manual return=%+v", g)
		}
	}
}

const ae1IP = "203.0.113.10"

func ae1AutoRuntime(t *testing.T, limit int) (*KiloRuntime, map[string]kilo.EgressAccountStatus) {
	t.Helper()
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 1, Proxies: []config.KiloProxy{{ID: "p1", URL: "http://127.0.0.1:32101"}, {ID: "p2", URL: "http://127.0.0.1:32102"}, {ID: "public", URL: "http://127.0.0.1:32103"}}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, id := range []string{"p1", "p2", "public"} {
		ip := ae1IP
		if id == "public" {
			ip = "203.0.113.20"
		}
		r.health[id] = kiloProxyHealth{checked: true, healthy: true, checkedAt: now, observedAt: now, observedIP: ip}
	}
	on := true
	if _, err = r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, AutoAssignmentEnabled: &on, MaxUniqueAccounts: &limit}, nil); err != nil {
		t.Fatal(err)
	}
	ip := ae1IP
	if _, err = r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, ID: "p1", FixedIP: &ip}, nil); err != nil {
		t.Fatal(err)
	}
	accounts := map[string]kilo.EgressAccountStatus{
		"a": {Provider: "kilo", CreatedAt: time.Now(), CanAllocate: true, RestrictionPersisted: true},
		"b": {Provider: "kilo", CreatedAt: time.Now(), CanAllocate: true, RestrictionPersisted: true},
	}
	return r, accounts
}

func TestAE1AutoExclusiveGroupDrainRetireRestore(t *testing.T) {
	r, accounts := ae1AutoRuntime(t, 1)
	r.ReconcileAuto(accounts, time.Now())
	g := r.state.IPGroups[ae1IP]
	if g.Owner != "a" || len(g.AccountIDs) != 1 || r.state.Accounts["b"].Mode != "auto" {
		t.Fatalf("assignment: %+v", g)
	}
	a := &coreauth.Auth{ID: "a", Provider: "kilo", Metadata: map[string]any{"allow_direct_when_proxy_full": true}}
	_, release1, wait, err := r.TryAcquire(a)
	if err != nil || wait {
		t.Fatalf("first lease: %v %v", wait, err)
	}
	_, release2, wait, err := r.TryAcquire(a)
	if err != nil || wait {
		t.Fatalf("same-IP second entry: %v %v", wait, err)
	}
	_, _, wait, err = r.TryAcquire(a)
	if err != nil || !wait {
		t.Fatalf("full group borrowed public/direct: %v %v", wait, err)
	}
	release1()
	release2()
	if r.state.IPGroups[ae1IP].Owner != "a" {
		t.Fatal("idle request released lifetime ownership")
	}
	if _, _, wait, err = r.TryAcquire(&coreauth.Auth{ID: "b", Provider: "kilo"}); err != nil || !wait {
		t.Fatal("waiting account borrowed occupied IP")
	}
	_, held, _, err := r.TryAcquire(a)
	if err != nil {
		t.Fatal(err)
	}
	x := accounts["a"]
	x.ReturnRequested = true
	x.CanAllocate = false
	x.RestrictionPersisted = false
	accounts["a"] = x
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups[ae1IP].Draining {
		t.Fatal("unpersisted restriction returned ownership")
	}
	x.RestrictionPersisted = true
	accounts["a"] = x
	r.ReconcileAuto(accounts, time.Now())
	if !r.state.IPGroups[ae1IP].Draining || len(r.state.Recycle) != 0 {
		t.Fatal("inflight lease was transferred")
	}
	x.ReturnRequested = false
	x.CanAllocate = true
	accounts["a"] = x
	r.ReconcileAuto(accounts, time.Now())
	if !r.state.IPGroups[ae1IP].Draining {
		t.Fatal("recovery canceled committed drain")
	}
	held()
	r.ReconcileAuto(accounts, time.Now())
	g = r.state.IPGroups[ae1IP]
	if g.Owner != "" || !g.Retired || len(g.AccountIDs) != 1 || len(r.state.Recycle) != 2 {
		t.Fatalf("retirement: %+v %+v", g, r.state.Recycle)
	}
	n := 2
	if _, err = r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, MaxUniqueAccounts: &n}, accounts); err != nil {
		t.Fatal(err)
	}
	r.ReconcileAuto(accounts, time.Now())
	if !r.state.IPGroups[ae1IP].Retired || len(r.state.Recycle) != 2 {
		t.Fatal("raising N silently restored retired IP")
	}
	p, err := r.ProxyForProbe(r.state.Revision, "p1", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CompleteProxyProbe(r.state.Revision, p, true, nil, ae1IP); err != nil {
		t.Fatal(err)
	}
	x.CanAllocate = false
	accounts["a"] = x
	r.ReconcileAuto(accounts, time.Now())
	g = r.state.IPGroups[ae1IP]
	if g.Owner != "b" || len(g.AccountIDs) != 2 {
		t.Fatalf("explicit restore failed: %+v", g)
	}
	loaded, managed, err := r.store.Load(kilo.ProxyState{})
	if err != nil || !managed || loaded.IPGroups[ae1IP].Owner != "b" || len(loaded.IPGroups[ae1IP].AccountIDs) != 2 {
		t.Fatalf("restart lost lifetime history: %+v %v", loaded.IPGroups, err)
	}
}

func TestAE1ManagementLeasePinAndSaveReservationBlockAssignment(t *testing.T) {
	r, accounts := ae1AutoRuntime(t, 3)
	_, release, err := r.AcquireQuotaManagement("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.state.IPGroups[ae1IP].AccountIDs) != 0 {
		t.Fatal("management query counted as formal assignment")
	}
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups[ae1IP].Owner != "" {
		t.Fatal("management lease was handed off")
	}
	release()
	r.devicePins = map[string]*kiloDevicePin{"device": {authorization: kilo.AuthorizationEgress{Mode: "pool", ProxyID: "p2"}}}
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups[ae1IP].Owner != "" {
		t.Fatal("authorization pin was handed off")
	}
	delete(r.devicePins, "device")
	r.state.PendingDevices = map[string]kilo.PendingDeviceSave{"pending": {Draft: kilo.InferenceEgress{Mode: "pool", PrimaryProxyID: "p2"}}}
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups[ae1IP].Owner != "" {
		t.Fatal("save reservation was handed off")
	}
	delete(r.state.PendingDevices, "pending")
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups[ae1IP].Owner != "a" {
		t.Fatal("free group not assigned")
	}
	// Only the separate public route remains available to management work.
	_, release, err = r.AcquireQuotaManagement("a")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.pool.Snapshot().Proxies {
		if p.ID != "public" && p.Active != 0 {
			t.Fatal("query borrowed an exclusive IP")
		}
	}
	release()
}

func TestAE1AutoHistoryDriftStorageAndDeviceDefault(t *testing.T) {
	r, accounts := ae1AutoRuntime(t, 3)
	x := accounts["b"]
	x.CanAllocate = false
	accounts["b"] = x
	r.ReconcileAuto(accounts, time.Now())
	x = accounts["a"]
	x.CanAllocate = false
	x.ReturnRequested = true
	accounts["a"] = x
	r.ReconcileAuto(accounts, time.Now())
	x.CanAllocate = true
	x.ReturnRequested = false
	accounts["a"] = x
	r.ReconcileAuto(accounts, time.Now())
	if len(r.state.IPGroups[ae1IP].AccountIDs) != 1 {
		t.Fatal("same AuthID counted twice")
	}
	p, _ := r.ProxyForProbe(r.state.Revision, "p2", false)
	if _, err := r.CompleteProxyProbe(r.state.Revision, p, false, nil, "203.0.113.99"); !errors.Is(err, kilo.ErrProxyHealth) {
		t.Fatal(err)
	}
	if _, _, wait, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"}); err != nil || !wait {
		t.Fatal("fixed-IP drift remained eligible")
	}
	// A failed state write leaves ownership and history unchanged.
	bad := filepath.Join(t.TempDir(), "not-directory")
	if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	r.store.Dir = bad
	x.ReturnRequested = true
	x.CanAllocate = false
	accounts["a"] = x
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups[ae1IP].Owner != "a" || r.autoError == nil {
		t.Fatal("storage failure transferred ownership")
	}
	// A fresh device has no inference assignment requirement at prepare/save time.
	r, accounts = ae1AutoRuntime(t, 3)
	_, draft, err := r.PrepareDevice("device", "new", kilo.AuthorizationEgress{Mode: "direct"}, kilo.InferenceEgress{Mode: "pool", Default: true}, func() {})
	if err != nil || draft.Mode != "auto" {
		t.Fatalf("device default: %+v %v", draft, err)
	}
	if err = r.BeginDeviceSave("device", "new", draft); err != nil {
		t.Fatal(err)
	}
	if err = r.FinishDeviceSave("new", true); err != nil {
		t.Fatal(err)
	}
	if r.state.Accounts["new"].Mode != "auto" || r.state.Accounts["new"].PrimaryProxyID != "" {
		t.Fatal("no-IP save did not remain waiting")
	}
}
