package codexinspection

import (
	"context"
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBPSInspectionReadOnlyAndUnknownQuota(t *testing.T) {
	for _, scenario := range []string{"allowed", "restricted", "unauthorized", "forbidden", "rate_limited", "invalid_response"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/v8/management/bps/credentials/access" || r.URL.Query().Get("auth_index") != "bps-one" {
					t.Error("BPS误走其他接口")
				}
				status := 200
				allowed := true
				payload := map[string]any{"provider": "bps", "auth_index": "bps-one", "auth_id": "bps.json", "queried_at": "2026-11-11T00:00:00Z", "allowed": allowed, "models": []string{"PAGE_MEMORY_ONLY"}, "upstream_status": status, "error": nil, "quota": nil, "inference_verified": false, "auto_recover_eligible": false}
				if scenario == "restricted" {
					payload["allowed"] = false
					payload["models"] = []string{}
				}
				if scenario != "allowed" && scenario != "restricted" {
					payload["allowed"] = nil
					payload["models"] = nil
					payload["error"] = scenario
				}
				_ = json.NewEncoder(w).Encode(payload)
			}))
			defer server.Close()
			s := &Service{client: server.Client()}
			result := s.inspectSingleAccount(context.Background(), store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "test-admin"}, model.DefaultCodexInspectionConfig(), account{Provider: "bps", AuthIndex: "bps-one", FileName: "bps.json", Disabled: true, AutoRecoverOwned: true}, runLogger{})
			if calls != 1 || result.Action != "keep" || result.UsedPercent != nil || result.IsQuota || result.AutoRecoverEligible || strings.Contains(result.ErrorDetail, "PAGE_MEMORY_ONLY") {
				t.Fatalf("错误的BPS结果: %+v calls=%d", result, calls)
			}
			for _, mode := range []string{"none", "enable", "disable", "delete"} {
				if allowAutoAction(mode, true, result) {
					t.Fatal("BPS进入自动动作")
				}
			}
		})
	}
}

func TestBPSRunPersistsUnknownQuotaWithoutAutomaticMutation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"name":"bps.json","auth_index":"bps-one","provider":"bps","disabled":true}]}`))
		case "/v8/management/bps/credentials/access":
			_ = json.NewEncoder(w).Encode(map[string]any{"provider": "bps", "auth_index": "bps-one", "auth_id": "bps.json", "queried_at": "2026-11-11T00:00:00Z", "allowed": true, "models": []string{"PAGE_ONLY"}, "upstream_status": 200, "error": nil, "quota": nil, "inference_verified": false, "auto_recover_eligible": false})
		default:
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	db := newCodexInspectionTestStore(t)
	cfg := newCodexInspectionManagerConfig(upstream.URL)
	cfg.CodexInspection.TargetTypes = []string{"bps"}
	cfg.CodexInspection.TargetType = "bps"
	cfg.CodexInspection.AutoActionMode = "delete"
	cfg.CodexInspection.AutoRecoverEnabled = true
	if err := db.SaveManagerConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	service := newCodexInspectionTestService(t, db)
	detail, err := service.Run(context.Background(), RunRequest{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := service.GetRun(context.Background(), detail.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Run.ProbeSetCount != 1 || len(persisted.Results) != 1 {
		t.Fatal(persisted)
	}
	result := persisted.Results[0]
	if result.Provider != "bps" || result.Action != "keep" || result.UsedPercent != nil || result.AutoRecoverEligible || !result.Disabled || strings.Contains(result.ErrorDetail, "PAGE_ONLY") {
		t.Fatal(result)
	}
}
