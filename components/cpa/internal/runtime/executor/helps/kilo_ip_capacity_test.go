package helps

import (
	"errors"
	"fmt"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func ux2Runtime(t *testing.T, n int) (*KiloRuntime, map[string]kilo.EgressAccountStatus) {
	t.Helper()
	proxies := []config.KiloProxy{}
	for i := 0; i < n; i++ {
		proxies = append(proxies, config.KiloProxy{ID: fmt.Sprintf("p%d", i), URL: fmt.Sprintf("http://127.0.0.1:%d", 32000+i)})
	}
	proxies = append(proxies, config.KiloProxy{ID: "alias", URL: "http://127.0.0.1:32999"})
	r, err := NewManagedKiloRuntime(&config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 10, Proxies: proxies}})
	if err != nil {
		t.Fatal(err)
	}
	next := r.state.Clone()
	next.AutoAssignmentEnabled = true
	next.MaxUniqueAccounts = 10
	next.IPGroups = map[string]kilo.IPGroup{}
	next.Accounts["a"] = kilo.AccountEgress{Mode: "auto", CapacityPolicy: kilo.CapacityPolicy{CapacityMode: "ip", MaxIPs: n}}
	for i := range next.Proxies {
		ip := fmt.Sprintf("203.0.113.%d", i+1)
		if next.Proxies[i].ID == "alias" {
			ip = "203.0.113.1"
		}
		next.Proxies[i].FixedIP = ip
		next.IPGroups[ip] = kilo.IPGroup{IP: ip, AccountIDs: []string{}}
		r.health[next.Proxies[i].ID] = kiloProxyHealth{checked: true, healthy: true, checkedAt: time.Now(), observedAt: time.Now(), observedIP: ip}
	}
	if _, err = r.commitState(next); err != nil {
		t.Fatal(err)
	}
	accounts := map[string]kilo.EgressAccountStatus{"a": {Provider: "kilo", CanAllocate: true, CreatedAt: time.Now(), RestrictionPersisted: true}}
	r.ReconcileAuto(accounts, time.Now())
	return r, accounts
}
func TestUX2CapacityAutoOneToFiveAndRestart(t *testing.T) {
	r, _ := ux2Runtime(t, 5)
	if len(r.state.OwnedIPs("a")) != 1 {
		t.Fatal("initial binding")
	}
	releases := []func(){}
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
	})
	for i := 0; i < 100; i++ {
		_, release, busy, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
		if err != nil || busy {
			t.Fatalf("admission %d busy=%v err=%v", i+1, busy, err)
		}
		releases = append(releases, release)
		if got := len(r.state.OwnedIPs("a")); got != i/20+1 {
			t.Fatalf("request %d bindings=%d", i+1, got)
		}
	}
	if _, release, busy, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"}); !busy || err != nil || release != nil {
		t.Fatal("101st exceeded capacity")
	}
	view := r.StateView()
	for _, g := range view.IPGroups {
		if g.InferenceActive != 20 || g.ManagementActive != 0 {
			t.Fatalf("IP counters=%+v", g)
		}
	}
	if view.Accounts[0].CurrentCapacity != 100 || view.Accounts[0].ConfiguredCapacity != 100 || view.Accounts[0].InferenceActive != 100 {
		t.Fatalf("account=%+v", view.Accounts[0])
	}
	if _, _, err := r.AcquireQuotaManagement("a"); err == nil {
		t.Fatal("management escaped group limit")
	}
	releases[0]()
	releases[0]()
	_, release, busy, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
	if err != nil || busy {
		t.Fatal("freed slot not reused")
	}
	releases = append(releases, release)
	for _, fn := range releases {
		fn()
	}
	for _, g := range r.StateView().IPGroups {
		if g.Active != 0 || g.Owner != "a" {
			t.Fatal("idle lifetime owner lost")
		}
	}
	loaded, managed, err := r.store.Load(kilo.ProxyState{})
	if err != nil || !managed || len(loaded.OwnedIPs("a")) != 5 || loaded.Accounts["a"].MaxIPs != 5 {
		t.Fatalf("reload=%+v %v", loaded, err)
	}
	t.Log("automatic 1->5 IPs; 100 leases; each IP20 incl shared alias; 101 waits; once-only release; persisted5 idle owners")
}
func TestUX2SpecifiedAtomicConflictStorageAndDrain(t *testing.T) {
	r, accounts := ux2Runtime(t, 3)
	accounts["b"] = kilo.EgressAccountStatus{Provider: "kilo"}
	next := r.state.Clone()
	g := next.IPGroups["203.0.113.3"]
	g.Owner = "b"
	g.AccountIDs = []string{"b"}
	next.IPGroups[g.IP] = g
	next.Accounts["b"] = kilo.AccountEgress{Mode: "auto"}
	if _, err := r.commitState(next); err != nil {
		t.Fatal(err)
	}
	before := r.state.Clone()
	c := kilo.ProxyChanges{ExpectedRevision: r.state.Revision, EgressAccount: "a", Mode: "auto", CapacityPolicy: kilo.CapacityPolicy{CapacityMode: "ip", AllocationMode: "specified", MaxIPs: 3, SpecifiedIPs: []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"}}}
	if _, err := r.ChangeState(c, accounts); !errors.Is(err, kilo.ErrStateConflict) || !reflect.DeepEqual(before, r.state) {
		t.Fatal("specified conflict changed existing state")
	}
	c.SpecifiedIPs = []string{"203.0.113.1", "203.0.113.2"}
	original := r.store.Dir
	bad := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	r.store.Dir = bad
	if _, err := r.ChangeState(c, accounts); err == nil || !reflect.DeepEqual(before, r.state) {
		t.Fatal("storage failure changed owner/history")
	}
	r.store.Dir = original
	if _, err := r.ChangeState(c, accounts); err != nil {
		t.Fatal(err)
	}
	if len(r.state.OwnedIPs("a")) != 2 {
		t.Fatal("specified set not complete")
	}
	releases := []func(){}
	t.Cleanup(func() {
		for _, fn := range releases {
			fn()
		}
	})
	for i := 0; i < 21; i++ {
		_, fn, busy, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
		if err != nil || busy {
			t.Fatal("lease absent")
		}
		releases = append(releases, fn)
	}
	c.ExpectedRevision = r.state.Revision
	c.AllocationMode = "auto"
	c.SpecifiedIPs = nil
	c.MaxIPs = 1
	if _, err := r.ChangeState(c, accounts); err != nil {
		t.Fatal(err)
	}
	if !r.state.IPGroups["203.0.113.2"].Draining || len(r.state.OwnedIPs("a")) != 2 {
		t.Fatal("draining did not count binding")
	}
	releases[20]()
	status := accounts["a"]
	status.Active = 20
	accounts["a"] = status
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups["203.0.113.2"].Owner != "" || r.state.IPGroups["203.0.113.1"].Owner != "a" {
		t.Fatal("per-IP drain blocked by unrelated account activity")
	}
	t.Log("specified transaction retains old set on conflict/storage failure; per-IP drain releases independently with20 other requests active")
}
func TestUX2CapacityManagementAndVisibleLowLimits(t *testing.T) {
	r, accounts := ux2Runtime(t, 1)
	limit := 7
	entry := 10
	a := accounts["a"]
	a.ManualLimit = &limit
	accounts["a"] = a
	if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, SetIPEntryLimit: true, IPEntryLimit: &entry}, accounts); err != nil {
		t.Fatal(err)
	}
	client, release, err := r.AcquireQuotaManagement("a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_ = client
	view := r.StateView()
	if view.Accounts[0].CurrentCapacity != 7 || view.Accounts[0].EffectiveLimit != 7 || view.Accounts[0].ManagementActive != 1 || view.Accounts[0].InferenceActive != 0 {
		t.Fatalf("low limit/projection=%+v", view.Accounts[0])
	}
	leases := []func(){}
	defer func() {
		for _, fn := range leases {
			fn()
		}
	}()
	for i := 0; i < 19; i++ {
		_, fn, busy, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
		if err != nil || busy {
			t.Fatalf("slot%d %v %v", i, busy, err)
		}
		leases = append(leases, fn)
	}
	if _, _, busy, _ := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"}); !busy {
		t.Fatal("management not counted in group capacity")
	}
	release()
	release()
	if r.StateView().IPGroups[0].ManagementActive != 0 {
		t.Fatal("management release duplicated")
	}
	t.Log("management1+inference19 shares20; manual7 visible; entry10 explicit; alias capacity never stacks beyond20")
}

func TestUX2DeviceSpecifiedConflictKeepsPolicyWithoutHistory(t *testing.T) {
	r, _ := ux2Runtime(t, 3)
	before := r.state.Clone()
	draft := kilo.InferenceEgress{Mode: "auto", CapacityPolicy: kilo.CapacityPolicy{CapacityMode: "ip", AllocationMode: "specified", MaxIPs: 3, SpecifiedIPs: []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"}}}
	_, draft, err := r.PrepareDevice("device-ux2", "new", kilo.AuthorizationEgress{Mode: "direct"}, draft, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.BeginDeviceSave("device-ux2", "new", draft); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.IPGroups, r.state.IPGroups) {
		t.Fatal("credential preparation wrote history")
	}
	if err = r.FinishDeviceSave("new", true); err != nil {
		t.Fatal(err)
	}
	if len(r.state.OwnedIPs("new")) != 0 || !reflect.DeepEqual(before.IPGroups, r.state.IPGroups) || r.state.Accounts["new"].AllocationMode != "specified" {
		t.Fatal("conflicted saved policy lost or partially allocated")
	}
	if status, reason := r.DeviceSaveEgress("new"); status != "pending" || reason != "specified_conflict" {
		t.Fatalf("save status=%s/%s", status, reason)
	}
	if err = r.VerifyDeviceSave("new", draft); err != nil {
		t.Fatal("saved credential retry policy rejected")
	}
	draft.ExpectedRevision = nil
	_, draft2, err := r.PrepareDevice("device-abort", "abort", kilo.AuthorizationEgress{Mode: "direct"}, draft, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.BeginDeviceSave("device-abort", "abort", draft2); err != nil {
		t.Fatal(err)
	}
	if err = r.FinishDeviceSave("abort", false); err != nil {
		t.Fatal(err)
	}
	if _, exists := r.state.Accounts["abort"]; exists || !reflect.DeepEqual(before.IPGroups, r.state.IPGroups) {
		t.Fatal("failed credential left policy/history")
	}
	t.Log("saved conflicted specified policy retained; zero partial owners/history; retry accepted; failed credential reservation removed")
}

func TestUX2SpecifiedPartialFaultKeepsRemainingCapacity(t *testing.T) {
	r, accounts := ux2Runtime(t, 6)
	ips := []string{"203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4", "203.0.113.5"}
	c := kilo.ProxyChanges{ExpectedRevision: r.state.Revision, EgressAccount: "a", Mode: "auto", CapacityPolicy: kilo.CapacityPolicy{CapacityMode: "ip", AllocationMode: "specified", MaxIPs: 5, SpecifiedIPs: ips}}
	if _, err := r.ChangeState(c, accounts); err != nil {
		t.Fatal(err)
	}
	h := r.health["p4"]
	h.healthy = false
	r.health["p4"] = h
	r.ReconcileAuto(accounts, time.Now())
	if r.state.IPGroups["203.0.113.5"].Owner != "" || !r.ipPolicyReady("a") {
		t.Fatal("partial fault stopped healthy specified IPs")
	}
	releases := []func(){}
	defer func() {
		for _, fn := range releases {
			fn()
		}
	}()
	for i := 0; i < 80; i++ {
		_, fn, busy, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
		if err != nil || busy {
			t.Fatalf("remaining capacity%d %v/%v", i, busy, err)
		}
		releases = append(releases, fn)
	}
	if _, _, busy, _ := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo", Metadata: map[string]any{"allow_direct_when_proxy_full": true}}); !busy {
		t.Fatal("partial set escaped specified capacity")
	}
	if r.state.IPGroups["203.0.113.6"].Owner != "" || r.StateView().Accounts[0].CurrentCapacity != 80 || r.StateView().Accounts[0].AllocationReason != "partial_egress_unavailable" || r.StateView().Accounts[0].AssignedIP != "203.0.113.1" {
		t.Fatal("borrowed unspecified IP or incorrect current capacity")
	}
	for _, fn := range releases {
		fn()
	}
	h = r.health["p4"]
	h.healthy = true
	h.checkedAt = time.Now()
	r.health["p4"] = h
	r.ReconcileAuto(accounts, time.Now())
	if len(r.state.OwnedIPs("a")) != 5 || r.StateView().Accounts[0].CurrentCapacity != 100 || len(r.state.IPGroups["203.0.113.5"].AccountIDs) != 1 {
		t.Fatal("recovered specified IP not restored without extra history")
	}
	for id, h := range r.health {
		h.healthy = false
		r.health[id] = h
	}
	r.ReconcileAuto(accounts, time.Now())
	if r.ipPolicyReady("a") {
		t.Fatal("all failed set remained ready")
	}
	t.Log("specified5 complete -> fault1 -> healthy4 admit80; failedIP0, extraIP0/direct0; originalIP recovers to5/100 with history1; all failed waits")
}

func TestUX2SwitchFixedDrainsExtraIPAndRetainsHistory(t *testing.T) {
	r, accounts := ux2Runtime(t, 2)
	releases := []func(){}
	defer func() {
		for _, fn := range releases {
			fn()
		}
	}()
	for i := 0; i < 21; i++ {
		_, fn, busy, err := r.TryAcquire(&coreauth.Auth{ID: "a", Provider: "kilo"})
		if err != nil || busy {
			t.Fatal("lease absent")
		}
		releases = append(releases, fn)
	}
	status := accounts["a"]
	status.Active = 21
	accounts["a"] = status
	if _, err := r.ChangeState(kilo.ProxyChanges{ExpectedRevision: r.state.Revision, EgressAccount: "a", Mode: "auto", CapacityPolicy: kilo.CapacityPolicy{CapacityMode: "fixed"}}, accounts); err != nil {
		t.Fatal(err)
	}
	if !r.state.IPGroups["203.0.113.2"].Draining || len(r.state.OwnedIPs("a")) != 2 {
		t.Fatal("fixed switch transferred active owner")
	}
	releases[20]()
	r.ReconcileAuto(accounts, time.Now())
	g := r.state.IPGroups["203.0.113.2"]
	if g.Owner != "" || len(g.AccountIDs) != 1 || len(r.state.OwnedIPs("a")) != 1 {
		t.Fatal("fixed switch failed to retain singleIP/history")
	}
	t.Log("2IP->fixed auto: extra activeIP drains, after its own release exactly1 owner remains; history retained")
}
func TestUX2DeviceOwnPinTransferStorageFailureAndRetry(t *testing.T) {
	r, _ := ux2Runtime(t, 3)
	draft := kilo.InferenceEgress{Mode: "auto", CapacityPolicy: kilo.CapacityPolicy{CapacityMode: "ip", AllocationMode: "specified", MaxIPs: 2, SpecifiedIPs: []string{"203.0.113.2", "203.0.113.3"}}}
	revision := r.state.Revision
	_, draft, err := r.PrepareDevice("device-transfer", "new", kilo.AuthorizationEgress{Mode: "pool", ProxyID: "p1", ExpectedRevision: &revision}, draft, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.BeginDeviceSave("device-transfer", "new", draft); err != nil {
		t.Fatal(err)
	}
	before := r.state.Clone()
	original := r.store.Dir
	bad := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(bad, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	r.store.Dir = bad
	if err = r.FinishDeviceSave("new", true, "device-transfer"); err == nil || !reflect.DeepEqual(before, r.state) || !r.proxyPinned("p1") {
		t.Fatal("failed save lost pin or mutated ownership/history")
	}
	r.store.Dir = original
	if err = r.FinishDeviceSave("new", true, "device-transfer"); err != nil {
		t.Fatal(err)
	}
	if len(r.state.OwnedIPs("new")) != 2 || !r.proxyPinned("p1") {
		t.Fatal("same session pin not handed over atomically")
	}
	r.ReleaseDevice("device-transfer")
	t.Log("own authorization pin permits exact set commit; storage failure preserves pin/pending/history and retry completes2IP")
}
