package cliproxy

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestAE1NativeFixedConfirmationRejectsManualCombination(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "203.0.113.10") }
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":0}`, 200)
	m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", fmt.Sprintf(`{"expected_revision":0,"fixed_exit_ip":"203.0.113.10","account_id":%q}`, id), 409)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(state, "revision").Uint() != 0 || len(ojson.GetBytes(state, "ip_groups").Array()) != 0 || ojson.GetBytes(state, "proxies.0.account_id").String() != "" {
		t.Fatalf("conflicting confirmation mutated state: %s", state)
	}
	m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", `{"expected_revision":0,"fixed_exit_ip":"203.0.113.10"}`, 200)
	for _, fields := range []string{`"primary_proxy_id":"local-proxy"`, `"primary_proxy_id":"","candidate_proxy_ids":["local-proxy"]`} {
		m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":1,"mode":"pool","exclusive_proxy_ids":[],`+fields+`}`, 409)
	}
}

func TestAE1NativeExclusiveInferencePauseRecoveryAndExactDrain(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 3 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 4 {
			_, _ = io.WriteString(w, "data: "+m6QuotaFailure+"\n\n")
			return
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("ae1-response-%d", n))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "203.0.113.10") }
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":0,"auto_assignment_enabled":true,"max_unique_accounts":2}`, 200)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":1}`, 200)
	m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", `{"expected_revision":1,"fixed_exit_ip":"203.0.113.10"}`, 200)
	id := m2Create(t, f, "")
	m5Codex(t, f, "source-infer.json", "outer", "synthetic-outer", false, false)
	m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"source-infer.json","chatgpt_account_id":"outer"}`)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/source-policy", `{"expected_policy_revision":0,"enabled":true,"interval_seconds":1800}`, 200)
	var used atomic.Int32
	used.Store(100)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":%d,"reset_after_seconds":300}}}`, used.Load())
	}
	request := func(session string) (int, []byte) {
		return kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", session, `{"model":"gpt-6.1-sol","input":"test"}`)
	}
	if status, body := request("ae1-first"); status != 200 {
		t.Fatalf("first inference=%d %s", status, body)
	}
	m5Assert(t, m5Query(t, f, id, 1), "success", "")
	f.manager.SyncKiloAutoEgress(time.Now())
	if status, _ := request("ae1-paused"); status < 400 || calls.Load() != 1 {
		t.Fatal("source-paused request escaped admission")
	}
	used.Store(1)
	m5Assert(t, m5Query(t, f, id, 1), "success", "")
	f.manager.SyncKiloAutoEgress(time.Now())
	if status, body := request("ae1-recovered"); status != 200 {
		t.Fatalf("recovered inference=%d %s", status, body)
	}
	done := make(chan int, 1)
	go func() { status, _ := request("ae1-held"); done <- status }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("held inference absent")
	}
	if status, _ := request("ae1-exhausted"); status < 400 || calls.Load() != 4 {
		t.Fatal("exact quota replayed or succeeded")
	}
	f.manager.SyncKiloAutoEgress(time.Now())
	state, err := f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.IPGroups) != 1 || !state.IPGroups[0].Draining || state.IPGroups[0].Owner != id {
		t.Fatalf("active inference transferred: %+v", state.IPGroups)
	}
	if status, _ := request("ae1-blocked"); status < 400 || calls.Load() != 4 {
		t.Fatal("exact quota admitted or replayed")
	}
	once.Do(func() { close(release) })
	if status := <-done; status != 200 {
		t.Fatalf("held inference interrupted: %d", status)
	}
	assertUX1BrowserReleased(t, f, 4)
	f.manager.SyncKiloAutoEgress(time.Now())
	state, err = f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	if state.IPGroups[0].Owner != "" || state.IPGroups[0].Draining || state.IPGroups[0].UniqueAccounts != 1 || f.manager.KiloRestriction(id).Reason != "quota_exhausted" || calls.Load() != 4 || f.posts.Load() != 4 || f.connects.Load() == 0 {
		t.Fatalf("exact drain result: state=%+v restriction=%+v calls=%d posts=%d", state.IPGroups, f.manager.KiloRestriction(id), calls.Load(), f.posts.Load())
	}
}

func TestAE1NativePolicyRecoveryWithoutInferenceIP(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "203.0.113.10") }
	poolURL := "/v8/management/kilo/proxy-pool"
	m2Request(t, f, "PATCH", poolURL, `{"expected_revision":0,"auto_assignment_enabled":true}`, 400)
	m2Request(t, f, "PATCH", poolURL, `{"expected_revision":0,"max_unique_accounts":0}`, 400)
	m2Request(t, f, "PATCH", poolURL, `{"expected_revision":0,"auto_assignment_enabled":true,"max_unique_accounts":1}`, 200)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":1}`, 200)
	m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", `{"expected_revision":1,"fixed_exit_ip":"203.0.113.10"}`, 200)
	a := m2Create(t, f, `,"name":"auto-a"`)
	b := m2Create(t, f, `,"name":"auto-b"`)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	g := ojson.GetBytes(state, "ip_groups.0")
	if g.Get("owner_account_id").String() != a || g.Get("unique_accounts").Int() != 1 {
		t.Fatalf("new account auto assignment: %s", state)
	}
	bAuth, _ := f.manager.GetByID(b)
	if _, _, wait, err := f.executor.TryAcquireEgress(bAuth); err != nil || !wait {
		t.Fatal("second account borrowed IP")
	}
	m5Codex(t, f, "source.json", "outer", "synthetic-outer", false, false)
	m5Bind(t, f, a, 0, `{"kind":"cpa_codex","credential_id":"source.json","chatgpt_account_id":"outer"}`)
	policyURL := kiloAccountsURL + "/" + a + "/source-policy"
	m2Request(t, f, "PUT", policyURL, `{"expected_policy_revision":0,"enabled":true,"interval_seconds":1800}`, 200)
	var used atomic.Int32
	used.Store(100)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":%d,"reset_after_seconds":300},"secondary_window":{"limit_window_seconds":604800,"used_percent":1,"reset_after_seconds":600}}}`, used.Load())
	}
	m5Assert(t, m5Query(t, f, a, 1), "success", "")
	policy := m2Request(t, f, "GET", policyURL, "", 200)
	if !ojson.GetBytes(policy, "paused").Bool() {
		t.Fatalf("raw 100 not paused: %s", policy)
	}
	f.manager.SyncKiloAutoEgress(time.Now())
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if !ojson.GetBytes(state, "ip_groups.0.retired").Bool() || len(ojson.GetBytes(state, "recycle").Array()) != 1 {
		t.Fatalf("quota did not retire IP: %s", state)
	}
	used.Store(1)
	m5Assert(t, m5Query(t, f, a, 1), "success", "")
	policy = m2Request(t, f, "GET", policyURL, "", 200)
	if ojson.GetBytes(policy, "paused").Bool() {
		t.Fatalf("source's own management route failed recovery: %s", policy)
	}
	f.manager.SyncKiloAutoEgress(time.Now())
	aAuth, _ := f.manager.GetByID(a)
	if _, _, wait, err := f.executor.TryAcquireEgress(aAuth); err != nil || !wait {
		t.Fatal("recovered account escaped no-IP waiting")
	}
	if f.usageConnects.Load() != 2 || f.usageDirect.Load() != 0 || f.posts.Load() != 0 {
		t.Fatalf("source routing wire: CONNECT=%d direct=%d model=%d", f.usageConnects.Load(), f.usageDirect.Load(), f.posts.Load())
	}
	// Imported recovery material has no implicit direct fallback or exclusive-IP borrowing.
	m5Bind(t, f, a, 1, `{"kind":"imported","access_token":"synthetic-imported"}`)
	m5Assert(t, m5Query(t, f, a, 2), "failed", "query_egress_unavailable")
	if f.usageConnects.Load() != 2 || f.usageDirect.Load() != 0 || f.posts.Load() != 0 {
		t.Fatal("missing query route caused network fallback")
	}
}
