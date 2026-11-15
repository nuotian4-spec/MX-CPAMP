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
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloQuotaNativePoolNoDirectFallback(t *testing.T) {
	for _, kind := range []string{"shared", "exclusive", "full", "empty", "unhealthy"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.allowDirect = true
			id := m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
			m5Bind(t, f, id, 0, `{"kind":"imported","access_token":"synthetic-imported"}`)
			var calls atomic.Int32
			f.usageHandler = func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, m5UsageBody) }
			if kind == "empty" {
				m2Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":0}`, 200)
			} else {
				if kind == "exclusive" {
					m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":0,"mode":"pool","exclusive_proxy_ids":["local-proxy"],"primary_proxy_id":"local-proxy"}`, 200)
				}
				want := 200
				if kind == "unhealthy" {
					want = 502
					f.probeHandler = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }
				}
				view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
				m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(view, "revision").Uint()), want)
			}
			if kind == "full" {
				a, _ := f.manager.GetByID(id)
				for i := 0; i < 2; i++ {
					_, release, wait, err := f.executor.TryAcquireEgress(a)
					if err != nil || wait {
						t.Fatal("pool reserve")
					}
					defer release()
				}
			}
			view := m5Query(t, f, id, 1)
			if kind == "shared" {
				m5Assert(t, view, "success", "")
				if calls.Load() != 1 || f.usageConnects.Load() != 1 {
					t.Fatal("usage CONNECT missing")
				}
				for _, proxy := range f.executor.KiloProxySnapshot().Proxies {
					if proxy.Active != 0 {
						t.Fatal("HTTP lease retained")
					}
				}
			} else {
				m5Assert(t, view, "failed", "query_egress_unavailable")
				if calls.Load() != 0 {
					t.Fatal("unavailable pool sent usage")
				}
			}
			if f.usageDirect.Load() != 0 || f.posts.Load() != 0 {
				t.Fatal("usage full-pool fallback or model")
			}
		})
	}
}

func TestKiloQuotaNativeCanceledQueryReleasesHTTPLease(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":0}`, 200)
	m5Bind(t, f, id, 0, `{"kind":"imported","access_token":"synthetic-imported"}`)
	entered, closed := make(chan struct{}), make(chan struct{})
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(closed) }
	before, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+kiloAccountsURL+"/"+id+"/quota/query", strings.NewReader(`{"expected_binding_revision":1}`))
	req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
	req.Header.Set("Content-Type", "application/json")
	done := make(chan error, 1)
	go func() {
		r, err := f.server.Client().Do(req)
		if r != nil {
			_ = r.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("usage absent")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("caller cancel ignored")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream socket open")
	}
	m3Wait(t, func() bool {
		view, err := f.manager.KiloQuota(id)
		return err == nil && view.Query.Status == "cancelled"
	})
	view := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	m5Assert(t, view, "cancelled", "usage_cancelled")
	for _, proxy := range f.executor.KiloProxySnapshot().Proxies {
		if proxy.Active != 0 {
			t.Fatal("canceled query retains lease")
		}
	}
	after, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	if !bytes.Equal(before, after) || f.posts.Load() != 0 {
		t.Fatal("cancel mutated credential or generated")
	}
	t.Log("caller cancel closes real usage socket, releases HTTP lease, records cancelled without changing credential; model0")
}

func TestKiloQuotaNativeSourceFailuresAndMissingOuter(t *testing.T) {
	for _, kind := range []string{"missing_outer_grant", "siwc_only", "terminal", "invalid_proxy", "identity_unknown", "source_missing", "provider_changed", "account_changed"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			id := m2Create(t, f, "")
			m4oDirect(t, f, id)
			a := m5Codex(t, f, "codex-a.json", "outer-a", "synthetic-outer-old", true, false)
			m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-a.json","chatgpt_account_id":"outer-a"}`)
			var oauth, usages atomic.Int32
			f.oauthHandler = func(w http.ResponseWriter, r *http.Request) {
				oauth.Add(1)
				if r.URL.Path != "/oauth/token" || r.Method != "POST" {
					t.Error("outer OAuth route")
				}
				doc := ojson.NewDocString(`{"access_token":"synthetic-outer-current","refresh_token":"synthetic-refresh-current","expires_in":604800}`)
				_ = doc.Set("id_token", m5JWT("outer-a"))
				_, _ = w.Write(doc.Bytes())
			}
			f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
				usages.Add(1)
				if r.Header.Get("Authorization") != "Bearer synthetic-outer-current" {
					t.Error("missing outer did not refresh")
				}
				_, _ = io.WriteString(w, m5UsageBody)
			}
			switch kind {
			case "missing_outer_grant":
				a.Metadata["access_token"] = ""
			case "siwc_only":
				delete(a.Metadata, "access_token")
				delete(a.Metadata, "refresh_token")
				a.Metadata["siwc"] = map[string]any{"access_token": "synthetic-siwc-only", "refresh_token": "synthetic-siwc-refresh", "client_id": "synthetic-client", "expired": time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)}
			case "terminal":
				a.RequiresSignIn = true
				a.RejectedRefreshToken = "synthetic-refresh-codex-a.json"
			case "invalid_proxy":
				a.ProxyURL = "://invalid"
			case "identity_unknown":
				a.Metadata["account_id"] = ""
			case "source_missing":
				f.manager.RemoveIf(context.Background(), a.ID, func(*coreauth.Auth) bool { return true })
			case "provider_changed":
				a.Provider = "other"
				a.Metadata["type"] = "other"
			case "account_changed":
				a.Metadata["account_id"] = "other-account"
			}
			if kind != "source_missing" {
				if _, err := f.manager.Register(coreauth.WithAuthCreationIntent(context.Background()), a); err != nil {
					t.Fatal(err)
				}
			}
			view := m5Query(t, f, id, 1)
			if kind == "missing_outer_grant" {
				m5Assert(t, view, "success", "")
				if oauth.Load() != 1 || usages.Load() != 1 {
					t.Fatal("outer grant skipped")
				}
			} else {
				if ojson.GetBytes(view, "query.status").String() != "failed" || oauth.Load() != 0 || usages.Load() != 0 {
					t.Fatalf("source failure touched network: %s", view)
				}
			}
			if f.posts.Load() != 0 {
				t.Fatal("source resolver called model")
			}
		})
	}
	t.Run("known_imported_expiry", func(t *testing.T) {
		f := newKiloNativeFixture(t, kiloSuccessStream, true)
		id := m2Create(t, f, "")
		m4oDirect(t, f, id)
		doc := ojson.NewDocString(`{}`)
		_ = doc.Set("exp", time.Now().Add(-time.Hour).Unix())
		token := "e30." + base64.RawURLEncoding.EncodeToString(doc.Bytes()) + ".synthetic"
		source := ojson.NewDocString(`{"kind":"imported"}`)
		_ = source.Set("access_token", token)
		m5Bind(t, f, id, 0, source.String())
		m5Assert(t, m5Query(t, f, id, 1), "failed", "needs_reimport")
		if f.usageDirect.Load() != 0 || f.posts.Load() != 0 {
			t.Fatal("expired material requested upstream")
		}
	})
}

func TestKiloQuotaNativeInvalidResponses(t *testing.T) {
	for _, kind := range []string{"malformed", "over_limit", "redirect", "network"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			id := m2Create(t, f, "")
			m4oDirect(t, f, id)
			m5HealthyQueryRoute(t, f)
			m5Bind(t, f, id, 0, `{"kind":"imported","access_token":"synthetic-imported"}`)
			var calls atomic.Int32
			f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch kind {
				case "malformed":
					_, _ = io.WriteString(w, "{broken")
				case "over_limit":
					_, _ = io.WriteString(w, strings.Repeat("x", 128*1024+1))
				case "redirect":
					w.Header().Set("Location", "https://forbidden.invalid/private-upstream-secret")
					w.WriteHeader(302)
				case "network":
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
				}
			}
			view := m5Query(t, f, id, 1)
			if ojson.GetBytes(view, "query.status").String() != "failed" || ojson.GetBytes(view, "query.observed_at").Type != ojson.Null || len(ojson.GetBytes(view, "windows").Array()) != 0 || ojson.GetBytes(view, "credits.remaining").Type != ojson.Null || calls.Load() != 1 || f.posts.Load() != 0 {
				t.Fatalf("invalid response projected as observation: %s", view)
			}
		})
	}
}

func TestKiloQuotaNativeLateRefreshKeepsOriginalJournal(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	m5Codex(t, f, "codex-a.json", "outer-a", "synthetic-outer-old", true, false)
	m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-a.json","chatgpt_account_id":"outer-a"}`)
	entered, release := make(chan struct{}), make(chan struct{})
	var refreshes, usages atomic.Int32
	f.oauthHandler = func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		close(entered)
		<-release
		doc := ojson.NewDocString(`{"access_token":"synthetic-outer-late","refresh_token":"synthetic-refresh-late","expires_in":604800}`)
		_ = doc.Set("id_token", m5JWT("outer-a"))
		_, _ = w.Write(doc.Bytes())
	}
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) { usages.Add(1); _, _ = io.WriteString(w, m5UsageBody) }
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", kiloAccountsURL+"/"+id+"/quota/query", kiloManagementKey, `{"expected_binding_revision":1}`)
		done <- status
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh absent")
	}
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id+"/quota-source", `{"expected_binding_revision":1}`, 200)
	close(release)
	if code := <-done; code != 409 {
		t.Fatalf("late refresh quota status=%d", code)
	}
	a, _ := f.manager.GetByID("codex-a.json")
	if a.Metadata["access_token"] != "synthetic-outer-late" {
		t.Fatal("original refresh transaction was discarded")
	}
	receipts, err := filepath.Glob(filepath.Join(f.cfg.AuthDir, ".codex-refresh", "*", "receipt"))
	if err != nil || len(receipts) != 1 {
		t.Fatal("original journal receipt missing")
	}
	view := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	m5Assert(t, view, "never", "")
	if usages.Load() != 0 || refreshes.Load() != 1 || f.posts.Load() != 0 {
		t.Fatal("late refresh issued usage/model")
	}
	t.Log("unlink while original OAuth in flight: refresh1/journal receipt1 committed to Codex, quota409, usage0/model0")
}

func TestKiloQuotaNativeSIWCOnlyDueUsesOuter(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	a := m5Codex(t, f, "codex-a.json", "outer-a", "synthetic-outer-current", false, true)
	a.Metadata["siwc"].(map[string]any)["expired"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
	if _, err := f.manager.Register(coreauth.WithAuthCreationIntent(context.Background()), a); err != nil {
		t.Fatal(err)
	}
	var siwc, usages atomic.Int32
	f.oauthHandler = func(w http.ResponseWriter, r *http.Request) {
		siwc.Add(1)
		if r.URL.Path != "/api/accounts/oauth/token" || r.Method != "POST" {
			t.Error("valid outer was refreshed")
		}
		_, _ = io.WriteString(w, `{"access_token":"synthetic-siwc-current","refresh_token":"synthetic-siwc-refresh-current","expires_in":604800}`)
	}
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		usages.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-outer-current" {
			t.Error("SIWC token used for usage")
		}
		_, _ = io.WriteString(w, m5UsageBody)
	}
	m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-a.json","chatgpt_account_id":"outer-a"}`)
	m5Assert(t, m5Query(t, f, id, 1), "success", "")
	if siwc.Load() != 1 || usages.Load() != 1 || f.posts.Load() != 0 {
		t.Fatal("partition counts")
	}
	t.Log("valid outer + SIWC due: outerOAuth0/SIWCOAuth1/usage1 using original outer/model0")
}
