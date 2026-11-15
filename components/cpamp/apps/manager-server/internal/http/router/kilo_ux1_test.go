package router

import (
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKiloUX1PublicProjectionAndDeviceDraft(t *testing.T) {
	quota := `{"account_id":"fixture-account","binding_revision":2,"source":{"kind":"cpa_codex","credential_id":"source-real","status":"ready","token":"SECRET"},"query":{"status":"success","attempted_at":null,"observed_at":"2026-11-13T01:02:03Z","error_code":null},"plan":null,"windows":[],"credits":{"remaining":null,"source_field":null,"unit":null},"stale":false,"connection_ref":"UPSTREAM_REF"}`
	summary := `{"kind":"cpa_codex","credential_id":"source-real","display_label":"同名账号","status":"ready","binding_revision":2,"token":"SECRET"}`
	account := strings.TrimSuffix(kiloSavedFixture, "}") + `,"quota_source_summary":` + summary + `,"quota_snapshot":` + quota + `}`
	proxy := strings.Replace(kiloProxyFixture, `"owner":"a1"`, `"owner":"a1","owner_kind":"account","observed_exit_ip":"198.51.100.12","observed_at":"2026-11-13T01:02:03Z","observation_status":"ok"`, 1)
	proxy = strings.Replace(proxy, `"primary_proxy_id":"p1"`, `"primary_proxy_id":"p1","candidate_proxy_ids":["p1","p2"]`, 1)
	draft := map[string]any{"mode": "pool", "candidate_proxy_ids": []any{"p1", "p2"}, "exclusive_proxy_ids": []any{}, "primary_proxy_id": "p1", "expected_revision": float64(7)}
	starts := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			io.WriteString(w, kiloRouterConcurrencyFixture)
		case "/v8/management/kilo/accounts":
			io.WriteString(w, `{"accounts":[`+account+`]}`)
		case "/v8/management/kilo/proxies":
			io.WriteString(w, proxy)
		case "/v8/management/kilo/device-auth":
			starts++
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Fatal("草稿正文无效")
			}
			if _, ok := body["connection_ref"]; ok {
				t.Error("连接引用未剥离")
			}
			raw, _ := json.Marshal(body["inference_egress"])
			want, _ := json.Marshal(draft)
			if string(raw) != string(want) {
				t.Errorf("候选草稿未完整透传:%s", raw)
			}
			pin := body["authorization_egress"].(map[string]any)
			if pin["proxy_id"] != "p2" || pin["expected_revision"] != float64(7) {
				t.Error("授权pin未透传")
			}
			encoded, _ := json.Marshal(draft)
			io.WriteString(w, strings.TrimSuffix(kiloDeviceStartFixture, "}")+`,"authorization_egress":{"mode":"pool","proxy_id":"p2","token":"SECRET"},"inference_egress":`+string(encoded)+`}`)
		default:
			t.Errorf("意外请求:%s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloAccountsRef(t, h)
	rec := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	var list model.KiloAccounts
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Accounts) != 1 {
		t.Fatalf("列表投影失败:%d %s", rec.Code, rec.Body.String())
	}
	assertKiloRouterSanitized(t, rec)
	if list.Accounts[0].QuotaSnapshot.ConnectionRef != ref || list.Accounts[0].QuotaSourceSummary.CredentialID != "source-real" {
		t.Fatal("额度连接或稳定来源错误")
	}
	state := kiloProxySnapshot(t, h)
	if state.Proxies[0].ObservedExitIP == nil || *state.Proxies[0].ObservedExitIP != "198.51.100.12" || len(state.Accounts[0].CandidateProxyIDs) != 2 {
		t.Fatal("公网或候选投影缺失")
	}
	fields := kiloDeviceFields("start", ref)
	fields["authorization_egress"] = map[string]any{"mode": "pool", "proxy_id": "p2", "expected_revision": 7}
	fields["inference_egress"] = draft
	rec = kiloAccountsWrite(h, "POST", "/api/kilo/device-auth", kiloRouterAdminKey, fields)
	if rec.Code != 200 {
		t.Fatalf("设备草稿失败:%d %s", rec.Code, rec.Body.String())
	}
	assertKiloRouterSanitized(t, rec)
	var started model.KiloDeviceStarted
	if json.Unmarshal(rec.Body.Bytes(), &started) != nil || started.AuthorizationEgress == nil || started.AuthorizationEgress.ProxyID != "p2" || started.InferenceEgress == nil || len(started.InferenceEgress.CandidateProxyIDs) != 2 {
		t.Fatal("公开pin/草稿回执缺失")
	}
	for _, invalid := range []map[string]any{
		{"mode": "pool", "candidate_proxy_ids": []string{}, "exclusive_proxy_ids": []string{}, "primary_proxy_id": "", "expected_revision": 7},
		{"mode": "pool", "candidate_proxy_ids": []string{"p1"}, "exclusive_proxy_ids": []string{"p2"}, "primary_proxy_id": "p1", "expected_revision": 7},
		{"mode": "direct", "candidate_proxy_ids": []string{"p1"}, "exclusive_proxy_ids": []string{}, "primary_proxy_id": "", "expected_revision": 7},
	} {
		fields["inference_egress"] = invalid
		rec = kiloAccountsWrite(h, "POST", "/api/kilo/device-auth", kiloRouterAdminKey, fields)
		if rec.Code != 400 {
			t.Fatalf("无效草稿未拒绝:%d", rec.Code)
		}
	}
	if starts != 1 {
		t.Fatalf("无效草稿触发了上游:%d", starts)
	}
}
