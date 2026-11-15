package oauthlogin

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBPSReplacementRejectsBeforeWorker(t *testing.T) {
	var workerCalls, oauthCalls atomic.Int32
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/config/plugins" {
			jsonReply(w, map[string]any{"enabled": false, "configs": map[string]any{"cpa-bps": map[string]any{"enabled": true}}})
			return
		}
		oauthCalls.Add(1)
		jsonReply(w, map[string]any{"url": "https://example.invalid/authorize", "state": "test-state"})
	}))
	defer cpa.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workerCalls.Add(1)
		jsonReply(w, map[string]any{"result_cleared": true})
	}))
	defer worker.Close()
	s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "test-key"}}, nil, worker.URL, "worker-key")
	in := Input{Profile: "bps", Email: "user@example.invalid", Password: "synthetic", TOTPSecret: "ABCDEFGHIJKLMNOP"}
	if _, err := s.Create(in); err == nil {
		t.Error("BPS task accepted")
	}
	if _, err := s.CreateBatch(BatchInput{BatchID: "11111111-1111-4111-8111-111111111111", Profile: "bps", Items: []BatchInputItem{{LineNo: 1, Email: in.Email, Password: in.Password, TOTPSecret: in.TOTPSecret}}}); err == nil {
		t.Error("BPS batch accepted")
	}
	if _, err := s.OAuth(context.Background(), http.MethodPost, "auth-url", map[string]string{"provider": "bps"}); err == nil {
		t.Error("BPS OAuth accepted")
	}
	if workerCalls.Load() != 0 || oauthCalls.Load() != 0 {
		t.Errorf("upstream work started: worker=%d oauth=%d", workerCalls.Load(), oauthCalls.Load())
	}
}

func serveBuiltinBPSConfig(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v8/management/config/plugins" {
		return false
	}
	jsonReply(w, map[string]any{"configs": map[string]any{}})
	return true
}

func TestBPSLoginModeContract(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"missing", `{"error":"not_found"}`, "builtin", 404},
		{"off", `{"configs":{"cpa-bps":{"enabled":false}}}`, "builtin", 200},
		{"global_off", `{"enabled":false,"configs":{"cpa-bps":{"enabled":true}}}`, "plugin", 200},
		{"failure", `{}`, "unavailable", 500},
		{"null_config", `null`, "unavailable", 200},
		{"wrong_shape", `{"configs":{"cpa-bps":{"enabled":"true"}}}`, "unavailable", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v8/management/config/plugins" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("config request contract")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer cpa.Close()
			s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "test-key"}}, nil, "", "")
			if got := s.BPSLoginMode(context.Background()); got != tc.want {
				t.Fatalf("mode=%s want=%s", got, tc.want)
			}
		})
	}
}
