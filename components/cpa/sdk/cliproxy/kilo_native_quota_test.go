package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const m5UsageBody = `{"plan_type":"plus","rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":25,"reset_after_seconds":300},"secondary_window":{"limit_window_seconds":604800,"used_percent":50,"reset_after_seconds":600}},"additional_rate_limits":[{"rate_limit":{"primary_window":{"limit_window_seconds":2592000,"used_percent":10,"reset_after_seconds":900}}}],"spend_control":{"individual_limit":{"remaining":12.5}},"secret":"private-usage-secret"}`

func m5HealthyQueryRoute(t *testing.T, f *kiloNativeFixture) {
	t.Helper()
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(state, "revision").Uint()), 200)
}

func m5Codex(t *testing.T, f *kiloNativeFixture, id, account, token string, expired, siwc bool) *coreauth.Auth {
	t.Helper()
	exp := time.Now().Add(7 * 24 * time.Hour)
	if expired {
		exp = time.Now().Add(-time.Hour)
	}
	meta := map[string]any{"type": "codex", "account_id": account, "access_token": token, "refresh_token": "synthetic-refresh-" + id, "expired": exp.Format(time.RFC3339), "label": "same-name"}
	if siwc {
		meta["use_siwc"] = true
		meta["siwc"] = map[string]any{"access_token": "synthetic-siwc-old", "refresh_token": "synthetic-siwc-refresh-" + id, "client_id": "synthetic-issued-client", "device_id": "synthetic-device", "expired": exp.Format(time.RFC3339)}
	}
	a := &coreauth.Auth{ID: id, FileName: id, Provider: "codex", Label: "same-name", Status: coreauth.StatusActive, Metadata: meta, ProxyURL: f.cfg.Kilo.Proxies[0].URL, Attributes: map[string]string{coreauth.AttributePath: filepath.Join(f.cfg.AuthDir, id), coreauth.AttributeSource: filepath.Join(f.cfg.AuthDir, id), coreauth.AttributeSourceBackend: coreauth.AuthSourceFile}}
	result, err := f.manager.Register(coreauth.WithAuthCreationIntent(context.Background()), a)
	if err != nil {
		t.Fatal(err)
	}
	f.manager.RegisterExecutor(native.NewCodexExecutor(f.cfg))
	return result
}
func m5Bind(t *testing.T, f *kiloNativeFixture, id string, revision uint64, source string) []byte {
	t.Helper()
	return m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/quota-source", fmt.Sprintf(`{"expected_binding_revision":%d,"source":%s}`, revision, source), 200)
}
func m5Query(t *testing.T, f *kiloNativeFixture, id string, revision uint64) []byte {
	t.Helper()
	return m2Request(t, f, "POST", kiloAccountsURL+"/"+id+"/quota/query", fmt.Sprintf(`{"expected_binding_revision":%d}`, revision), 200)
}
func m5Assert(t *testing.T, body []byte, status, code string) {
	t.Helper()
	if ojson.GetBytes(body, "query.status").String() != status || ojson.GetBytes(body, "query.error_code").String() != code {
		t.Fatalf("quota state=%s", body)
	}
	for _, secret := range []string{"synthetic-outer", "synthetic-refresh", "synthetic-imported", "synthetic-siwc", "private-usage-secret", "private-upstream-secret"} {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatal("quota returned secret")
		}
	}
}
func m5JWT(account string) string {
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("https://api\\.openai\\.com/auth.chatgpt_account_id", account)
	return "e30." + base64.RawURLEncoding.EncodeToString(doc.Bytes()) + ".synthetic"
}

func TestKiloQuotaNativeReferenceRefreshAndRestart(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	m5Codex(t, f, "codex-a.json", "outer-a", "synthetic-outer-old", true, true)
	m5Codex(t, f, "codex-b.json", "outer-b", "synthetic-outer-b", false, false)
	var outer, siwc, usage atomic.Int32
	f.oauthHandler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("OAuth protocol")
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Error("OAuth grant")
		}
		if r.URL.Path == "/oauth/token" {
			outer.Add(1)
			if r.Form.Get("refresh_token") != "synthetic-refresh-codex-a.json" || r.Form.Get("scope") != "openid profile email" {
				t.Error("outer form")
			}
			doc := ojson.NewDocString(`{"access_token":"synthetic-outer-current","refresh_token":"synthetic-refresh-current","expires_in":604800}`)
			_ = doc.Set("id_token", m5JWT("outer-a"))
			_, _ = w.Write(doc.Bytes())
		} else if r.URL.Path == "/api/accounts/oauth/token" {
			siwc.Add(1)
			if r.Form.Get("client_id") != "synthetic-issued-client" || r.Form.Get("resource") != "https://api.openai.com/v1" || r.Form.Get("refresh_token") != "synthetic-siwc-refresh-codex-a.json" {
				t.Error("SIWC form")
			}
			_, _ = io.WriteString(w, `{"access_token":"synthetic-siwc-current","refresh_token":"synthetic-siwc-refresh-current","expires_in":604800}`)
		} else {
			t.Error("unexpected OAuth path")
			w.WriteHeader(500)
		}
	}
	expected := atomic.Value{}
	expected.Store("synthetic-outer-current")
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		usage.Add(1)
		if r.Method != "GET" || r.URL.Path != "/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer "+expected.Load().(string) || r.Header.Get("ChatGPT-Account-Id") != "outer-a" {
			t.Error("usage must use current outer only")
		}
		_, _ = io.WriteString(w, m5UsageBody)
	}
	sources := m2Request(t, f, "GET", "/v8/management/kilo/quota-sources", "", 200)
	if len(ojson.GetBytes(sources, "sources").Array()) != 2 || strings.Count(string(sources), "same-name") != 2 {
		t.Fatal("same-name sources collapsed")
	}
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key"} {
		status, _ := f.request(t, "GET", "/v8/management/kilo/quota-sources", key, "")
		if status != 401 {
			t.Fatal("inference key reached quota management")
		}
	}
	m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-a.json","chatgpt_account_id":"outer-a"}`)
	if outer.Load()+siwc.Load()+usage.Load() != 0 {
		t.Fatal("binding performed network")
	}
	before := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	m5Assert(t, before, "never", "")
	result := m5Query(t, f, id, 1)
	m5Assert(t, result, "success", "")
	if len(ojson.GetBytes(result, "windows").Array()) != 3 || ojson.GetBytes(result, "credits.remaining").Float() != 12.5 || outer.Load() != 1 || siwc.Load() != 1 || usage.Load() != 1 || f.oauthConnects.Load() != 1 || f.usageDirect.Load() != 0 || f.usageConnects.Load() != 1 {
		t.Fatalf("refresh/usage counts %d/%d/%d OAuthCONNECT=%d usageDirect=%d", outer.Load(), siwc.Load(), usage.Load(), f.oauthConnects.Load(), f.usageDirect.Load())
	}
	raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("synthetic-outer")) || bytes.Contains(raw, []byte("synthetic-refresh")) || bytes.Contains(raw, []byte("synthetic-siwc")) {
		t.Fatal("reference copied source material")
	}
	current, _ := f.manager.GetByID("codex-a.json")
	if current.Metadata["access_token"] != "synthetic-outer-current" {
		t.Fatal("original refresh not committed")
	}
	current.CodexRefreshPatch = nil
	current.Storage = nil
	current.ClearFileSnapshot()
	current.Metadata["access_token"] = "synthetic-outer-rotated"
	expected.Store("synthetic-outer-rotated")
	if _, err = f.manager.Register(coreauth.WithAuthCreationIntent(context.Background()), current); err != nil {
		t.Fatal(err)
	}
	current, _ = f.manager.GetByID("codex-a.json")
	if current.Metadata["access_token"] != "synthetic-outer-rotated" {
		t.Fatal("rotation fixture was not committed")
	}
	m5Assert(t, m5Query(t, f, id, 1), "success", "")
	m2Reload(t, f)
	m5Assert(t, m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200), "never", "")
	m5Assert(t, m5Query(t, f, id, 1), "success", "")
	current, _ = f.manager.GetByID("codex-a.json")
	current.Metadata["account_id"] = "other-outer"
	_, _ = f.manager.Register(coreauth.WithAuthCreationIntent(context.Background()), current)
	current, _ = f.manager.GetByID("codex-a.json")
	if current.Metadata["account_id"] != "other-outer" {
		t.Fatal("replacement fixture was not committed")
	}
	view := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	if ojson.GetBytes(view, "source.status").String() != "source_changed" || !ojson.GetBytes(view, "stale").Bool() {
		t.Fatal("changed identity silently rebound")
	}
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id+"/quota-source", `{"expected_binding_revision":1}`, 200)
	if _, ok := f.manager.GetByID("codex-a.json"); !ok {
		t.Fatal("unlink deleted Codex")
	}
	if f.posts.Load() != 0 || len(f.hook.snapshot()) != 0 {
		t.Fatal("quota entered model path")
	}
	t.Logf("real Router refresh outer%d/SIWC%d OAuthCONNECT%d usageGET%d/direct%d model0; stable reference/restart and identity change fenced", outer.Load(), siwc.Load(), f.oauthConnects.Load(), usage.Load(), f.usageDirect.Load())
}

func TestKiloQuotaNativeImportedFailuresAndAtomicSource(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, `,"enabled":false`)
	m4oDirect(t, f, id)
	m5HealthyQueryRoute(t, f)
	var status atomic.Int32
	status.Store(200)
	var calls atomic.Int32
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer synthetic-imported" || r.Header.Get("ChatGPT-Account-Id") != "imported-account" {
			t.Error("imported protocol")
		}
		w.WriteHeader(int(status.Load()))
		if status.Load() == 200 {
			_, _ = io.WriteString(w, m5UsageBody)
		} else {
			_, _ = io.WriteString(w, "private-upstream-secret")
		}
	}
	m5Bind(t, f, id, 0, `{"kind":"imported","access_token":"synthetic-imported","chatgpt_account_id":"imported-account"}`)
	first := m5Query(t, f, id, 1)
	m5Assert(t, first, "success", "")
	observed := ojson.GetBytes(first, "query.observed_at").String()
	for _, item := range []struct {
		status int32
		code   string
	}{{403, "usage_forbidden"}, {429, "usage_rate_limited"}, {500, "usage_http_error"}, {401, "needs_reimport"}} {
		status.Store(item.status)
		body := m5Query(t, f, id, 1)
		m5Assert(t, body, "failed", item.code)
		if !ojson.GetBytes(body, "stale").Bool() || ojson.GetBytes(body, "query.observed_at").String() != observed || ojson.GetBytes(body, "credits.remaining").Float() != 12.5 {
			t.Fatal("failed attempt overwrote observation")
		}
		if item.status != 401 && ojson.GetBytes(body, "source.status").String() != "ready" {
			t.Fatal("network permission was labeled revoked")
		}
	}
	a, _ := f.manager.GetByID(id)
	if !a.Disabled {
		t.Fatal("quota changed manual state")
	}
	path := filepath.Join(f.cfg.AuthDir, id)
	before, _ := os.ReadFile(path)
	f.manager.SetStore(m2FailStore{Store: m2FileStore(f), save: true})
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/quota-source", `{"expected_binding_revision":1,"source":{"kind":"imported","access_token":"synthetic-imported-replacement"}}`, 503)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed save changed source")
	}
	f.manager.SetStore(m2FileStore(f))
	for _, body := range []string{`{}`, `{"expected_binding_revision":null}`, `{"expected_binding_revision":1.1}`, `{"expected_binding_revision":1,"refresh_token":"x"}`, `{"expected_binding_revision":1,"source":{"kind":"imported","access_token":"x","refresh_token":"x"}}`, `{"expected_binding_revision":1,"source":{"kind":"cpa_codex","credential_id":"x"}}`} {
		m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/quota-source", body, 400)
	}
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id+"/quota-source", `{"expected_binding_revision":0}`, 409)
	m5Bind(t, f, id, 1, `{"kind":"imported","access_token":"synthetic-imported-replacement"}`)
	view := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	m5Assert(t, view, "never", "")
	if ojson.GetBytes(view, "query.observed_at").Type != ojson.Null {
		t.Fatal("replacement retained prior observation")
	}
	if f.oauthConnects.Load() != 0 || f.posts.Load() != 0 {
		t.Fatal("imported source refreshed/generated")
	}
	t.Logf("imported usageGET%d; refresh0/model0; 403/429 preserve ready,401 needs_reimport, persistence failure keeps original", calls.Load())
}

func TestKiloQuotaNativeLateResponsesAndBusy(t *testing.T) {
	for _, action := range []string{"unbind", "replace", "manual", "manual_roundtrip", "kilo_identity", "source_rotation", "source_delete", "provider_change", "source_account"} {
		t.Run(action, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			id := m2Create(t, f, "")
			m4oDirect(t, f, id)
			m5Codex(t, f, "codex-a.json", "outer-a", "synthetic-outer-current", false, false)
			m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-a.json","chatgpt_account_id":"outer-a"}`)
			if action == "manual_roundtrip" {
				m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/source-policy", `{"expected_policy_revision":0,"enabled":true,"interval_seconds":1800}`, 200)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				body := m5UsageBody
				if action == "manual_roundtrip" {
					body = strings.Replace(body, `"used_percent":25`, `"used_percent":90`, 1)
				}
				_, _ = io.WriteString(w, body)
			}
			done := make(chan int, 1)
			go func() {
				code, _ := f.request(t, "POST", kiloAccountsURL+"/"+id+"/quota/query", kiloManagementKey, `{"expected_binding_revision":1}`)
				done <- code
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("usage not entered")
			}
			m2Request(t, f, "POST", kiloAccountsURL+"/"+id+"/quota/query", `{"expected_binding_revision":1}`, 409)
			switch action {
			case "unbind":
				m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id+"/quota-source", `{"expected_binding_revision":1}`, 200)
			case "replace":
				m5Bind(t, f, id, 1, `{"kind":"imported","access_token":"synthetic-imported"}`)
			case "manual":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
			case "manual_roundtrip":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":true}`, 200)
			case "kilo_identity":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 200)
			case "source_delete":
				f.manager.RemoveIf(context.Background(), "codex-a.json", func(a *coreauth.Auth) bool { return a.ID == "codex-a.json" })
			default:
				a, _ := f.manager.GetByID("codex-a.json")
				if action == "source_rotation" {
					a.Metadata["access_token"] = "synthetic-outer-rotated"
				}
				if action == "provider_change" {
					a.Provider = "other"
				}
				if action == "source_account" {
					a.Metadata["account_id"] = "other"
				}
				_, _ = f.manager.Register(coreauth.WithAuthCreationIntent(context.Background()), a)
			}
			close(release)
			if code := <-done; code != 409 {
				t.Fatalf("late status=%d", code)
			}
			view := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
			if ojson.GetBytes(view, "query.observed_at").Type != ojson.Null || ojson.GetBytes(view, "query.status").String() == "running" {
				t.Fatal("late observation published or busy retained")
			}
			if action == "manual_roundtrip" {
				policy := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/source-policy", "", 200)
				a, _ := f.manager.GetByID(id)
				if a.Disabled || ojson.GetBytes(policy, "paused").Bool() || len(ojson.GetBytes(view, "windows").Array()) != 0 {
					t.Fatal("人工停用再启用后发布了迟到窗口或暂停副作用")
				}
			}
			if f.posts.Load() != 0 {
				t.Fatal("quota called model")
			}
		})
	}
}
