package codexinspection

import (
	"context"
	"fmt"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCPABPSInspectionFiltersPersistsAndKeepsReadOnly(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("巡检发送写请求", r.Method)
		}
		switch r.URL.Path {
		case "/v0/management/auth-files", "/v8/management/credentials":
			fmt.Fprint(w, `{"files":[{"id":"one.json","name":"one.json","auth_index":"one","provider":"cpa-bps","disabled":true},{"name":"old.json","auth_index":"old","provider":"bps"},{"name":"codex.json","auth_index":"codex","provider":"codex"}]}`)
		case "/v8/management/plugins/cpa-bps/quota":
			calls++
			fmt.Fprint(w, `{"subscription":{"plan":"team"},"groups":[{"buckets":[{"window":"300m","remainingFraction":0.75,"resetTime":"2100-01-01T00:00:00Z"}]}]}`)
		default:
			t.Error("非只读额度接口", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	db := newCodexInspectionTestStore(t)
	cfg := newCodexInspectionManagerConfig(server.URL)
	cfg.CodexInspection.TargetTypes = []string{"cpa-bps"}
	cfg.CodexInspection.TargetType = "cpa-bps"
	cfg.CodexInspection.AutoActionMode = "delete"
	cfg.CodexInspection.AutoRecoverEnabled = true
	if err := db.SaveManagerConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	service := newCodexInspectionTestService(t, db)
	run, err := service.Run(context.Background(), RunRequest{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := service.GetRun(context.Background(), run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(persisted.Results) != 1 {
		t.Fatalf("calls=%d %+v", calls, persisted)
	}
	result := persisted.Results[0]
	if result.Provider != "cpa-bps" || result.Action != "keep" || result.AutoRecoverEligible || !result.Disabled || result.UsedPercent == nil || *result.UsedPercent != 25 || result.IsQuota {
		t.Fatalf("%+v", result)
	}
	for _, action := range []string{"enable", "disable", "delete"} {
		r := model.CodexInspectionResult{Provider: "cpa-bps", Action: action, AutoRecoverEligible: true}
		if allowAutoAction(action, true, r) {
			t.Fatal("只读provider允许自动动作")
		}
	}
}

func TestCPABPSInspectionDiscardsChangedConnection(t *testing.T) {
	db := newCodexInspectionTestStore(t)
	cfg := newCodexInspectionManagerConfig("http://initial.invalid")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials" {
			fmt.Fprint(w, `{"files":[{"id":"one.json","auth_index":"one","provider":"cpa-bps"}]}`)
			return
		}
		changed := cfg
		changed.CPAConnection.ManagementKey = "changed"
		if err := db.SaveManagerConfig(context.Background(), changed); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"groups":[{"buckets":[{"remainingFraction":0.5}]}]}`)
	}))
	defer server.Close()
	cfg = newCodexInspectionManagerConfig(server.URL)
	if err := db.SaveManagerConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	service := newCodexInspectionTestService(t, db)
	_, setup, err := service.resolveRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := service.inspectSingleAccount(context.Background(), setup, model.DefaultCodexInspectionConfig(), account{Provider: "cpa-bps", AuthIndex: "one", RuntimeID: "one.json", FileName: "one.json"}, runLogger{})
	if result.ErrorKind != "cpa_target_changed" || result.UsedPercent != nil || len(result.QuotaWindows) != 0 || result.Action != "keep" {
		t.Fatal(result)
	}
}
