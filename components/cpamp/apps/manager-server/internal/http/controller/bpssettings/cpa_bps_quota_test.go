package bpssettings

import (
	"net/http/httptest"
	"testing"
)

func TestCPABPSQuotaRequiresAdminAndReference(t *testing.T) {
	for _, tc := range []struct {
		admin        bool
		method, path string
		code         int
	}{{false, "GET", "/api/cpa-bps/quota?auth_index=one&connection_ref=x", 401}, {true, "GET", "/api/cpa-bps/quota?auth_index=one", 400}, {true, "POST", "/api/cpa-bps/quota", 405}} {
		w := httptest.NewRecorder()
		(&Handler{Auth: bpsAdmin(tc.admin)}).PluginQuota(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
