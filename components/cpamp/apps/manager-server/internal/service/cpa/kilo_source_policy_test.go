package cpa

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAE1SourcePolicyAccountSummaries(t *testing.T) {
	const policy = `{"enabled":false,"interval_seconds":1800,"revision":0,"paused":false,"pause_generation":0,"scope":"","status":"disabled","observed_at":null,"next_query_at":null,"error_code":null,"persisted":true,"connection_ref":"UPSTREAM_REF","token":"SECRET"}`
	for _, action := range []string{"list", "create", "save"} {
		for _, bad := range []bool{false, true} {
			t.Run(action+map[bool]string{true: "坏回执", false: "合法回执"}[bad], func(t *testing.T) {
				p := policy
				if bad {
					p = strings.Replace(p, `"status":"disabled"`, `"status":"SECRET"`, 1)
				}
				account := `{"id":"a","effective_limit":1,"active":0,"waiting":0,"quota_source":"none","egress_mode":"pool","egress_status":"available","source_policy":` + p + `}`
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if action == "list" {
						io.WriteString(w, `{"accounts":[`+account+`]}`)
					} else {
						w.WriteHeader(201)
						io.WriteString(w, account)
					}
				}))
				defer up.Close()
				var result any
				var err error
				switch action {
				case "list":
					result, err = FetchKiloAccounts(context.Background(), up.Client(), up.URL, "SYNTHETIC")
				case "create":
					result, err = WriteKiloAccount(context.Background(), up.Client(), up.URL, "SYNTHETIC", "POST", "", nil)
				case "save":
					result, err = RequestKiloDevice(context.Background(), up.Client(), up.URL, "SYNTHETIC", "save", "d", nil)
				}
				if bad {
					if err == nil {
						t.Fatal("坏summary被接收")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(result)
				if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "UPSTREAM_REF") {
					t.Fatal("summary透传额外字段")
				}
				if !strings.Contains(string(raw), `"source_policy"`) {
					t.Fatal("summary策略丢失")
				}
			})
		}
	}
}
