package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloManagementColdHealthSingleSend(t *testing.T) {
	for _, kind := range []string{"quota", "organizations"} {
		for _, outcome := range []string{"healthy", "failed", "canceled"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				f := newKiloNativeFixture(t, kiloSuccessStream, true)
				id := m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
				m5Bind(t, f, id, 0, `{"kind":"imported","access_token":"synthetic-imported"}`)
				entered, release, probeDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
				f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
					close(entered)
					<-release
					defer close(probeDone)
					if outcome == "failed" {
						w.WriteHeader(502)
						return
					}
					_, _ = io.WriteString(w, "127.0.0.2")
				}
				var calls atomic.Int32
				f.usageHandler = func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, m5UsageBody) }
				f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = io.WriteString(w, `{"organizations":[],"hasPersonalAccount":true}`)
				}
				method, path, body := "GET", kiloAccountsURL+"/"+id+"/organizations", ""
				if kind == "quota" {
					method, path, body = "POST", kiloAccountsURL+"/"+id+"/quota/query", `{"expected_binding_revision":1}`
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, method, f.server.URL+path, strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
				req.Header.Set("Content-Type", "application/json")
				type response struct {
					status int
					body   []byte
					err    error
				}
				done := make(chan response, 1)
				go func() {
					res, err := f.server.Client().Do(req)
					v := response{err: err}
					if res != nil {
						v.status = res.StatusCode
						v.body, _ = io.ReadAll(res.Body)
						_ = res.Body.Close()
					}
					done <- v
				}()
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("health did not start")
				}
				if outcome == "canceled" {
					cancel()
				}
				if outcome != "canceled" {
					close(release)
				}
				var got response
				select {
				case got = <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("management request did not finish")
				}
				if outcome == "canceled" {
					close(release)
				}
				<-probeDone
				if outcome == "healthy" {
					if got.err != nil || got.status != 200 || calls.Load() != 1 {
						t.Fatalf("cold %s failed: status=%d calls=%d body=%s error=%v", kind, got.status, calls.Load(), got.body, got.err)
					}
					if kind == "quota" {
						m5Assert(t, got.body, "success", "")
					}
				} else if calls.Load() != 0 {
					t.Fatal("failed or canceled admission sent business HTTP")
				}
				if outcome == "failed" && kind == "quota" {
					m5Assert(t, got.body, "failed", "egress_unavailable")
				}
				if outcome == "failed" && kind == "organizations" && got.status < 400 {
					t.Fatal("failed health published organizations")
				}
				if outcome == "canceled" && got.err == nil {
					t.Fatal("cancellation ignored")
				}
				f.assertReleased(t, 0)
				if f.posts.Load() != 0 || f.usageDirect.Load() != 0 || f.deviceDirect.Load() != 0 || f.probes.Load() != 1 {
					t.Fatal("management admission replayed, generated or fell back direct")
				}
			})
		}
	}
}

func TestKiloManagementQuotaBindingListProjection(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m5Codex(t, f, "m10-projection.json", "outer-m10", "synthetic-outer", false, false)
	for revision, source := range []string{`{"kind":"cpa_codex","credential_id":"m10-projection.json","chatgpt_account_id":"outer-m10"}`, `{"kind":"imported","access_token":"synthetic-imported"}`, `{"kind":"none"}`} {
		if ojson.Get(source, "kind").String() == "none" {
			m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id+"/quota-source", fmt.Sprintf(`{"expected_binding_revision":%d}`, revision), 200)
		} else {
			m5Bind(t, f, id, uint64(revision), source)
		}
		list := m2Request(t, f, "GET", kiloAccountsURL, "", 200)
		account := ojson.GetBytes(list, "accounts.0")
		wantSource := []string{"codex_ref", "imported", "none"}[revision]
		if account.Get("quota_source").String() != wantSource || account.Get("codex_credential_id").String() != ojson.Get(source, "credential_id").String() {
			t.Fatalf("binding %d projection mismatch: %s", revision, list)
		}
	}
	if f.posts.Load() != 0 || f.usageConnects.Load() != 0 {
		t.Fatal(fmt.Sprintf("projection sent external calls: model=%d usage=%d", f.posts.Load(), f.usageConnects.Load()))
	}
}
