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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func m4oDirect(t *testing.T, f *kiloNativeFixture, id string) {
	t.Helper()
	f.allowDirect = true
	view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, ojson.GetBytes(view, "revision").Uint()), 200)
}
func m4oProfile(t *testing.T, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != "GET" || r.URL.Path != "/api/profile" || len(body) != 0 || r.Header.Get("Accept") != "application/json" || r.Header.Get("Authorization") != "Bearer "+kiloInitialKey || r.Header.Get("X-KILOCODE-MACHINEID") != "" || r.Header.Get("X-KiloCode-OrganizationId") != "" {
			t.Error("profile protocol mismatch")
		}
		_, _ = io.WriteString(w, `{"organizations":[{"id":"org_first","name":"First"},{"id":"org_second","name":"Second"},{"id":"invalid space","name":"skip"},{"id":1},null],"hasPersonalAccount":true,"private_token":"hidden-profile-secret"}`)
	}
}
func TestKiloOrganizationsNativeProfileSaveHeadersAndBinding(t *testing.T) {
	seen := make(chan kiloSessionSeen, 10)
	f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen), true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	var profiles atomic.Int32
	f.profileHandler = m4oProfile(t, &profiles)
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key"} {
		status, _ := f.request(t, "GET", kiloAccountsURL+"/"+id+"/organizations", key, "")
		if status != 401 {
			t.Fatal("profile management access not enforced")
		}
	}
	choices := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/organizations", "", 200)
	if len(ojson.GetBytes(choices, "organizations").Array()) != 2 || !ojson.GetBytes(choices, "has_personal_account").Bool() || bytes.Contains(choices, []byte("hidden-profile-secret")) {
		t.Fatalf("profile projection=%s", choices)
	}
	if f.posts.Load() != 0 {
		t.Fatal("profile called model")
	}
	a, _ := f.manager.GetByID(id)
	machine := coreauth.KiloMachineID(a)
	initialRevision := coreauth.KiloIdentity(a).Revision
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":"org_first"}`, 200)
	f.expectedOrganization.Store("org_first")
	code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "org-session", `{"model":"gpt-6.1-sol","input":"first org"}`)
	if code != 200 {
		t.Fatalf("first inference=%d %s", code, body)
	}
	first := <-seen
	f.assertReleased(t, 1)
	if first.Account != machine {
		t.Fatal("machine changed on organization save")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":"org_second"}`, 200)
	f.expectedOrganization.Store("org_second")
	a, _ = f.manager.GetByID(id)
	if coreauth.KiloIdentity(a).Revision != initialRevision+2 || coreauth.KiloMachineID(a) != machine {
		t.Fatal("organization identity revision/UUID mismatch")
	}
	for _, request := range []string{`{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":"old response"}`, `{"model":"gpt-6.1-sol","input":[{"type":"function_call_output","call_id":"call-resp-chain-1","output":"old tool"}]}`} {
		status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", request)
		if status != 409 {
			t.Fatalf("old binding accepted=%d", status)
		}
	}
	status, _ := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "org-session", `{"model":"gpt-6.1-sol","input":"old session"}`)
	if status != 409 {
		t.Fatalf("old session accepted=%d", status)
	}
	if f.posts.Load() != 1 {
		t.Fatal("old organization binding reached upstream")
	}
	status, body = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"new independent"}`)
	if status != 200 {
		t.Fatalf("new org=%d %s", status, body)
	}
	if next := <-seen; next.Account != machine {
		t.Fatal("second org changed UUID")
	}
	f.assertReleased(t, 2)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":""}`, 200)
	f.expectedOrganization.Store("")
	m2Reload(t, f)
	status, body = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"personal after reload"}`)
	if status != 200 {
		t.Fatalf("personal=%d %s", status, body)
	}
	if next := <-seen; next.Account != machine {
		t.Fatal("personal/reload changed UUID")
	}
	f.assertReleased(t, 3)
	a, _ = f.manager.GetByID(id)
	if coreauth.KiloIdentity(a).Organization != "" || coreauth.KiloMachineID(a) != machine {
		t.Fatal("personal state not durable")
	}
	if profiles.Load() != 4 || f.posts.Load() != 3 {
		t.Fatalf("profile/model=%d/%d", profiles.Load(), f.posts.Load())
	}
	t.Log("profileGET4; real modelPOST3 org_first/org_second/personal; old response/tool/session409 with zero extra upstream; UUID stable across reload")
}

func TestKiloOrganizationsCandidateKeyAndFailureIsolation(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	var profiles atomic.Int32
	f.profileHandler = m4oProfile(t, &profiles)
	m2Request(t, f, "POST", kiloDeviceURL, `{"name":"invalid start","authorization_egress":{"mode":"direct"},"organization_id":"org_first"}`, 400)
	m2Request(t, f, "POST", kiloAccountsURL, `{"api_key":"synthetic-kilo-token","organization_id":"org_first"}`, 400)
	if profiles.Load() != 0 {
		t.Fatal("invalid creation reached profile")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":"org_first"}`, 200)
	before, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := f.manager.GetByID(id)
	identity := coreauth.KiloIdentity(original)
	f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
		profiles.Add(1)
		if r.Header.Get("Authorization") != "Bearer candidate-new-key" {
			t.Error("did not validate pending key")
		}
		_, _ = io.WriteString(w, `{"organizations":[{"id":"org_other"}],"hasPersonalAccount":false}`)
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"candidate-new-key"}`, 403)
	f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
		profiles.Add(1)
		_, _ = io.WriteString(w, `{"organizations":[{"id":"org_first"}],"hasPersonalAccount":false}`)
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":""}`, 403)
	for _, value := range []string{`null`, `1`, `"bad id"`, `"` + strings.Repeat("a", 129) + `"`} {
		m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":`+value+`}`, 400)
	}
	f.manager.SetStore(m2FailStore{Store: m2FileStore(f), save: true})
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":"org_first","name":"not saved"}`, 503)
	after, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	current, _ := f.manager.GetByID(id)
	if !bytes.Equal(before, after) || coreauth.KiloIdentity(current) != identity || current.Label == "not saved" || f.posts.Load() != 0 {
		t.Fatal("failure mutated credential or called model")
	}
}

func TestKiloOrganizationsConcurrentIdentityAndInflight(t *testing.T) {
	for _, kind := range []string{"key", "inflight"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			id := m2Create(t, f, "")
			m4oDirect(t, f, id)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				_, _ = io.WriteString(w, `{"organizations":[{"id":"org_first"}],"hasPersonalAccount":true}`)
			}
			done := make(chan int, 1)
			go func() {
				status, _ := f.request(t, "PATCH", kiloAccountsURL+"/"+id, kiloManagementKey, `{"organization_id":"org_first"}`)
				done <- status
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("profile not entered")
			}
			var releaseAccount func()
			if kind == "key" {
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 200)
			} else {
				var err error
				releaseAccount, err = f.manager.AcquireCredentialGeneration(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				defer releaseAccount()
			}
			once.Do(func() { close(release) })
			select {
			case status := <-done:
				if status != 409 {
					t.Fatalf("stale profile save=%d", status)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("profile held global gate")
			}
			a, _ := f.manager.GetByID(id)
			if coreauth.KiloIdentity(a).Organization != "" {
				t.Fatal("stale profile changed organization")
			}
			if kind == "key" && a.Metadata["api_key"] != "synthetic-replaced-token" {
				t.Fatal("stale profile reverted key")
			}
			if f.posts.Load() != 0 {
				t.Fatal("profile operation called model")
			}
		})
	}
}

func TestKiloOrganizationsProfileFailuresNeverMutateOrInfer(t *testing.T) {
	for _, kind := range []string{"status", "malformed", "oversize", "network"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			id := m2Create(t, f, "")
			m4oDirect(t, f, id)
			f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "status":
					http.Error(w, "secret-profile-body", 403)
				case "malformed":
					_, _ = io.WriteString(w, `{"organizations":123,"token":"secret-profile-body"}`)
				case "oversize":
					_, _ = io.WriteString(w, strings.Repeat("x", 128*1024+1))
				case "network":
					c, _, _ := w.(http.Hijacker).Hijack()
					_ = c.Close()
				}
			}
			before, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
			want := 502
			if kind == "network" {
				want = 503
			}
			data := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/organizations", "", want)
			if bytes.Contains(data, []byte("secret-profile-body")) {
				t.Fatal("upstream secret leaked")
			}
			m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":"org_first"}`, want)
			after, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
			if !bytes.Equal(before, after) || f.posts.Load() != 0 {
				t.Fatal("profile failure mutated credential or inferred")
			}
		})
	}
}

func TestKiloOrganizationsPoolAndExclusiveNoDirectFallback(t *testing.T) {
	for _, kind := range []string{"shared", "exclusive", "full", "empty", "unhealthy"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.allowDirect = true
			id := m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
			var calls atomic.Int32
			f.profileHandler = m4oProfile(t, &calls)
			if kind == "empty" {
				m2Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":0}`, 200)
			} else {
				if kind == "exclusive" {
					m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":0,"mode":"pool","exclusive_proxy_ids":["local-proxy"],"primary_proxy_id":"local-proxy"}`, 200)
				}
				if kind == "unhealthy" {
					f.probeHandler = func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "bad", 502) }
				}
				view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
				want := 200
				if kind == "unhealthy" {
					want = 502
				}
				m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(view, "revision").Uint()), want)
			}
			if kind == "full" {
				a, _ := f.manager.GetByID(id)
				for i := 0; i < 2; i++ {
					_, release, wait, err := f.executor.TryAcquireEgress(a)
					if err != nil || wait {
						t.Fatal("pool reserve failed")
					}
					defer release()
				}
			}
			want := 503
			if kind == "shared" || kind == "exclusive" {
				want = 200
			}
			m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/organizations", "", want)
			if f.deviceDirect.Load() != 0 || f.posts.Load() != 0 {
				t.Fatal("profile used direct fallback or model")
			}
			if want == 200 {
				if calls.Load() != 1 || f.deviceConnects.Load() != 1 {
					t.Fatal("profile did not traverse real CONNECT")
				}
				for _, p := range f.executor.KiloProxySnapshot().Proxies {
					if p.Active != 0 {
						t.Fatal("profile lease retained")
					}
				}
			} else if calls.Load() != 0 {
				t.Fatal("unavailable pool sent profile")
			}
		})
	}
}

func TestKiloOrganizationsSlowProfileDoesNotBlockOtherAccount(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.cfg.Kilo.Proxies = append(f.cfg.Kilo.Proxies, config.KiloProxy{ID: "other-proxy", URL: f.cfg.Kilo.Proxies[0].URL})
	id := m2Create(t, f, `,"enabled":false`)
	m2Create(t, f, "")
	view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	for _, pid := range []string{"local-proxy", "other-proxy"} {
		view = m2Request(t, f, "POST", kiloProxiesURL+"/"+pid+"/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(view, "revision").Uint()), 200)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{"organizations":[{"id":"org_first"}],"hasPersonalAccount":true}`)
	}
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "PATCH", kiloAccountsURL+"/"+id, kiloManagementKey, `{"organization_id":"org_first"}`)
		done <- status
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("profile not entered")
	}
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"other account"}`)
	if status != 200 {
		t.Fatalf("other account blocked=%d %s", status, body)
	}
	once.Do(func() { close(release) })
	select {
	case status = <-done:
		if status != 200 {
			t.Fatalf("profile save=%d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("save not complete")
	}
	for _, p := range f.executor.KiloProxySnapshot().Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatal("profile/model leases retained")
		}
	}
}

func TestKiloOrganizationsOnlyChangePreservesAuthCooldown(t *testing.T) {
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "synthetic rejection", 401) }, true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	var calls atomic.Int32
	f.profileHandler = m4oProfile(t, &calls)
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"rejected"}`)
	if status != 401 {
		t.Fatalf("initial rejection=%d", status)
	}
	f.assertReleased(t, 1)
	a, _ := f.manager.GetByID(id)
	if a.LastError == nil || a.LastError.HTTPStatus != 401 {
		t.Fatal("real rejection not recorded")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"organization_id":"org_first"}`, 200)
	current, _ := f.manager.GetByID(id)
	if current.Unavailable != a.Unavailable || !current.NextRetryAfter.Equal(a.NextRetryAfter) || current.LastError == nil || current.LastError.HTTPStatus != 401 || current.RejectedAccessToken != a.RejectedAccessToken {
		t.Fatal("organization change cleared credential cooldown")
	}
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"still rejected"}`)
	if status != 503 || f.posts.Load() != 1 {
		t.Fatalf("organization edit bypassed rejection: status=%d posts=%d", status, f.posts.Load())
	}
}

func TestKiloOrganizationsCanceledProfilePreservesCredential(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":0}`, 200)
	entered, closed := make(chan struct{}), make(chan struct{})
	f.profileHandler = func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(closed) }
	before, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "PATCH", f.server.URL+kiloAccountsURL+"/"+id, strings.NewReader(`{"organization_id":"org_first"}`))
	req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
	req.Header.Set("Content-Type", "application/json")
	done := make(chan error, 1)
	go func() {
		response, err := f.server.Client().Do(req)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("profile not entered")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("inbound cancellation ignored")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not return")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream profile socket remained")
	}
	m3Wait(t, func() bool {
		for _, p := range f.executor.KiloProxySnapshot().Proxies {
			if p.Active != 0 {
				return false
			}
		}
		return true
	})
	after, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	a, _ := f.manager.GetByID(id)
	if !bytes.Equal(before, after) || coreauth.KiloIdentity(a).Organization != "" || f.posts.Load() != 0 {
		t.Fatal("canceled profile changed account or inferred")
	}
}
