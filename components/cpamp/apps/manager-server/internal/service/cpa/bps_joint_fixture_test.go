package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBPSJointManagerFixtures(t *testing.T) {
	dir := os.Getenv("BPS_JOINT_DIR")
	if dir == "" {
		t.Skip("需要CPA管理GET上一步共享输出")
	}
	for _, name := range []string{"missing", "null", "zero"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, name+".management-output.json"))
			if err != nil {
				t.Fatal(err)
			}
			getCalls := 0
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					posts++
					t.Error("非GET")
				}
				getCalls++
				switch r.URL.Path {
				case "/v8/management/credentials":
					fmt.Fprint(w, `{"files":[{"id":"synthetic-auth","auth_index":"synthetic-index","provider":"cpa-bps"}]}`)
				case "/v8/management/plugins/cpa-bps/quota":
					w.Write(raw)
				default:
					t.Error(r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			result, err := FetchCPABPSQuota(context.Background(), server.Client(), server.URL, "fixture", "synthetic-index", "synthetic-auth")
			if err != nil || getCalls != 3 || posts != 0 {
				t.Fatal(err, getCalls, posts)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, name+".manager-output.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			t.Log("管理GET原始响应直接作为HTTP响应体；Manager前后身份GET=2、quota GET=1、consume_POST=0")
		})
	}
}
