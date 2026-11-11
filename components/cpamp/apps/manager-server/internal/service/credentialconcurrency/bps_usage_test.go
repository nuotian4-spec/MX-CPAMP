package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBPSUsageBFFConnectionFence(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v8/management/bps/credentials/usage" || r.URL.Query().Get("auth_id") != "one.json" {
			t.Error("用量请求越界")
		}
		reply(w, map[string]any{"provider": "bps", "auth_index": "one", "auth_id": "one.json", "source": "chatgpt_account_usage", "queried_at": "2026-11-11T00:00:00Z", "upstream_status": 401, "error": "unauthorized", "plan_type": nil, "rate_limit": nil})
	}))
	defer server.Close()
	r := &resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "TEST"}}
	s := New(r, nil)
	ref, err := s.bindConnection(context.Background(), r.value)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.GetBPSUsage(context.Background(), "one", "one.json", ref)
	if err != nil || result.Error == nil || *result.Error != "unauthorized" || result.RateLimit != nil || calls != 1 {
		t.Fatal(result, err, calls)
	}
	r.value.ManagementKey = "changed"
	if _, err = s.GetBPSUsage(context.Background(), "one", "one.json", ref); err == nil || calls != 1 {
		t.Fatal("旧引用仍查询用量")
	}
}
