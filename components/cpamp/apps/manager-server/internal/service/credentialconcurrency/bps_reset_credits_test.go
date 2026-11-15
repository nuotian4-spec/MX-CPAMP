package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBPSResetCreditsConnectionFence(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		t.Run(map[bool]string{false: "正常及前置阻断", true: "返回前连接变化"}[mutate], func(t *testing.T) {
			calls := 0
			r := &resolver{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				calls++
				if q.Method != "GET" || q.URL.Path != "/v8/management/bps/credentials/quota" {
					t.Error("非只读次数入口")
				}
				if mutate {
					r.value.ManagementKey = "changed"
				}
				reply(w, map[string]any{"provider": "bps", "auth_index": "one", "auth_id": "one.json", "source": "chatgpt_account_reset_credits", "queried_at": "2026-11-15T00:00:00Z", "available_count": 0})
			}))
			defer server.Close()
			r.value = store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "fixture"}
			s := New(r, nil)
			ref, err := s.bindConnection(context.Background(), r.value)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.GetBPSResetCredits(context.Background(), "one", "one.json", ref)
			if mutate {
				if err == nil {
					t.Fatal("迟到连接结果被采纳")
				}
			} else {
				if err != nil || result.Count == nil || *result.Count != 0 {
					t.Fatal(result, err)
				}
			}
			r.value.ManagementKey = "changed-again"
			if _, err = s.GetBPSResetCredits(context.Background(), "one", "one.json", ref); err == nil || calls != 1 {
				t.Fatal("旧引用仍访问上游", err, calls)
			}
		})
	}
}
