package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBPSAccessBFFConnectionFence(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v8/management/bps/credentials/access" || r.Method != "GET" {
			t.Error("探针扩大请求")
		}
		reply(w, map[string]any{"provider": "bps", "auth_index": "bps-one", "auth_id": "bps.json", "queried_at": "2026-11-11T00:00:00Z", "allowed": true, "models": []string{"m"}, "upstream_status": 200, "error": nil, "quota": nil, "inference_verified": false, "auto_recover_eligible": false})
	}))
	defer server.Close()
	r := &resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "ADMIN"}}
	s := New(r, nil)
	ref, err := s.bindConnection(context.Background(), r.value)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.GetBPSAccess(context.Background(), "bps-one", ref)
	if err != nil || result.Allowed == nil || !*result.Allowed || calls != 1 {
		t.Fatal(result, err, calls)
	}
	r.value.ManagementKey = "changed"
	if _, err = s.GetBPSAccess(context.Background(), "bps-one", ref); err == nil || calls != 1 {
		t.Fatal("旧连接仍执行探针")
	}
}
