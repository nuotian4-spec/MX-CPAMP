package oauthlogin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type sessionContractTransport func(*http.Request) (*http.Response, error)

func (f sessionContractTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func sessionContractResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestNativeBPSSessionResultContract(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		confirmed  bool
	}{
		{"confirmed", `{"status":"ok","saved":true,"adopted":true,"auth_id":"synthetic-auth"}`, true},
		{"unconfirmed", `{"status":"ok","saved":true,"adopted":false}`, false},
		{"missing_fields", `{"status":"ok"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(&resolver{setup: store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}}, nil, "", "")
			s.sessions["state"] = &oauthSession{provider: "bps", setup: store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}, created: time.Now(), submitted: true}
			var calls atomic.Int32
			s.client.Transport = sessionContractTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return sessionContractResponse(tc.body), nil
			})
			out, err := s.OAuth(context.Background(), "GET", "status", map[string]string{"state": "state"})
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			_ = json.Unmarshal([]byte(tc.body), &raw)
			for _, key := range []string{"saved", "adopted", "auth_id"} {
				if out[key] != raw[key] {
					t.Fatalf("真实结果字段丢失 %s: %v", key, out)
				}
			}
			_, err = s.OAuth(context.Background(), "GET", "status", map[string]string{"state": "state"})
			if err != nil {
				t.Fatal(err)
			}
			want := int32(2)
			if tc.confirmed {
				want = 1
			}
			if calls.Load() != want {
				t.Fatalf("未确认结果不应固化: calls=%d want=%d", calls.Load(), want)
			}
		})
	}
}

func TestNativeBPSCancelArbitration(t *testing.T) {
	for _, first := range []string{"callback", "cancel"} {
		t.Run(first, func(t *testing.T) {
			s := New(&resolver{setup: store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}}, nil, "", "")
			setup := store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}
			s.sessions["state"] = &oauthSession{provider: "bps", setup: setup, created: time.Now()}
			entered, release := make(chan struct{}), make(chan struct{})
			var callbacks, deletes atomic.Int32
			s.client.Transport = sessionContractTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/plugins") {
					return sessionContractResponse(`{"configs":{}}`), nil
				}
				if r.Method == "DELETE" {
					deletes.Add(1)
					if first == "cancel" {
						close(entered)
						<-release
					}
					return sessionContractResponse(`{"status":"ok","cancelled":true}`), nil
				}
				callbacks.Add(1)
				if first == "callback" {
					close(entered)
					<-release
				}
				return sessionContractResponse(`{"status":"ok"}`), nil
			})
			callback := func() error {
				_, err := s.OAuth(context.Background(), "POST", "callback", map[string]string{"state": "state", "redirect_url": "https://callback.invalid/?state=state&code=synthetic"})
				return err
			}
			cancel := func() error {
				_, err := s.OAuth(context.Background(), "DELETE", "session", map[string]string{"state": "state"})
				return err
			}
			done := make(chan error, 1)
			if first == "callback" {
				go func() { done <- callback() }()
			} else {
				go func() { done <- cancel() }()
			}
			<-entered
			var err error
			if first == "callback" {
				err = cancel()
			} else {
				err = callback()
			}
			close(release)
			firstErr := <-done
			if firstErr != nil {
				t.Fatal(firstErr)
			}
			if !errors.Is(err, ErrBusy) && !errors.Is(err, ErrImporting) {
				t.Fatalf("并发相反操作未冲突: %v", err)
			}
			if first == "callback" && deletes.Load() != 0 || first == "cancel" && callbacks.Load() != 0 {
				t.Fatalf("冲突请求到了CPA: callbacks=%d deletes=%d", callbacks.Load(), deletes.Load())
			}
		})
	}
}

func TestNativeBPSCancelFalseRetainsSession(t *testing.T) {
	s := New(&resolver{setup: store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}}, nil, "", "")
	s.sessions["state"] = &oauthSession{provider: "bps", setup: store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}, created: time.Now()}
	s.client.Transport = sessionContractTransport(func(*http.Request) (*http.Response, error) {
		return sessionContractResponse(`{"status":"ok","cancelled":false}`), nil
	})
	out, err := s.OAuth(context.Background(), "DELETE", "session", map[string]string{"state": "state"})
	if err != nil || out["cancelled"] != false || s.sessions["state"] == nil {
		t.Fatalf("未取消却删会话: out=%v err=%v", out, err)
	}
}

func TestNativeBPSDiagnosticSource(t *testing.T) {
	if got := OAuthRequestDiagnostic("status", ErrNotFound); got["error_source"] != "manager_session" {
		t.Fatal(got)
	}
	if got := oauthOutcome(map[string]any{"error": "unknown or expired state"}, true); got["error_source"] != "cpa_session" {
		t.Fatal(got)
	}
	if got := oauthOutcome(map[string]any{"error": "oauth_exchange_failed", "save_outcome": "not_saved"}, true); got["error_source"] != "upstream" || got["save_outcome"] != "not_saved" {
		t.Fatal(got)
	}
}
