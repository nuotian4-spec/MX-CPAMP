package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCPABPSQuotaConnectionFence(t *testing.T) {
	for _, changeDuring := range []bool{false, true} {
		r := &resolver{}
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			calls++
			if req.URL.Path == "/v8/management/credentials" {
				reply(w, map[string]any{"files": []any{map[string]any{"id": "one.json", "auth_index": "one", "provider": "cpa-bps"}}})
			} else {
				if changeDuring {
					r.value.ManagementKey = "changed"
				}
				reply(w, map[string]any{})
			}
		}))
		r.value = store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "TEST"}
		s := New(r, nil)
		ref, err := s.bindConnection(context.Background(), r.value)
		if err != nil {
			t.Fatal(err)
		}
		if !changeDuring {
			r.value.ManagementKey = "changed"
		}
		_, err = s.GetCPABPSQuota(context.Background(), "one", "one.json", ref)
		server.Close()
		if err == nil || (!changeDuring && calls != 0) {
			t.Fatal("跨连接结果未拒绝", err, calls)
		}
	}
}
