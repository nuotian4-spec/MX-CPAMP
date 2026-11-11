package bpssettings

import (
	"net/http/httptest"
	"testing"
)

func TestBPSUsageRequiresAdminAndBoundReference(t *testing.T) {
	for _, test := range []struct {
		admin bool
		path  string
		code  int
	}{{false, "/api/bps/usage?auth_index=one&connection_ref=x", 401}, {true, "/api/bps/usage?auth_index=one", 400}} {
		w := httptest.NewRecorder()
		(&Handler{Auth: bpsAdmin(test.admin)}).Usage(w, httptest.NewRequest("GET", test.path, nil))
		if w.Code != test.code || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
