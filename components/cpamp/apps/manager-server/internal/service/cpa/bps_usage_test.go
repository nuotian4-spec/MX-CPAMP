package cpa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBPSUsageClientChecksBoundIdentityAndSource(t *testing.T) {
	for _, source := range []string{"chatgpt_account_usage", "other"} {
		t.Run(source, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v8/management/bps/credentials/usage" || r.URL.Query().Get("auth_index") != "one" || r.URL.Query().Get("auth_id") != "one.json" {
					t.Error("错误的用量请求")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"provider": "bps", "auth_index": "one", "auth_id": "one.json", "source": source, "queried_at": "2026-11-11T00:00:00Z", "upstream_status": 200, "error": nil, "plan_type": "plus", "rate_limit": map[string]any{"allowed": true, "limit_reached": false, "primary_window": map[string]any{"used_percent": 0, "limit_window_seconds": 2592000, "reset_after_seconds": nil, "reset_at": 1796977220}, "secondary_window": nil}})
			}))
			defer server.Close()
			result, err := FetchBPSUsage(context.Background(), server.Client(), server.URL, "TEST", "one", "one.json")
			if source == "other" {
				if err == nil {
					t.Fatal("错误来源被接受")
				}
				return
			}
			if err != nil || len(result.QuotaWindows()) != 1 || result.QuotaWindows()[0].UsedPercent == nil || *result.QuotaWindows()[0].UsedPercent != 0 {
				t.Fatal(result, err)
			}
		})
	}
}
