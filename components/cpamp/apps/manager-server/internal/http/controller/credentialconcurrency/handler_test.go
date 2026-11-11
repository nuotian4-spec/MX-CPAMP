package credentialconcurrency

import (
	"context"
	"encoding/json"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type auth bool

func (a auth) VerifyHeader(context.Context, string) (bool, error) { return bool(a), nil }
func TestAdminGateAndNarrowBody(t *testing.T) {
	for _, method := range []string{"GET", "PATCH", "POST"} {
		w := httptest.NewRecorder()
		(&Handler{Auth: auth(false)}).Handle(w, httptest.NewRequest(method, "/api/credential-concurrency", strings.NewReader(`{"token":"SECRET"}`)))
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{"auth_id":"a","concurrency_limit":20,"api_key":"SECRET"}`, `{} {}`} {
		w := httptest.NewRecorder()
		(&Handler{Auth: auth(true)}).Handle(w, httptest.NewRequest("PATCH", "/api/credential-concurrency", strings.NewReader(body)))
		if w.Code != 400 || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

type setupResolver struct{ setup store.Setup }

func (r setupResolver) ResolveSetup(context.Context) (store.Setup, bool, error) {
	return r.setup, true, nil
}
func TestSnapshotReferenceRoundTrip(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"mode":"local","default_limit":20,"pool_waiting":0,"credentials":[{"auth_id":"same","source":"file","editable":true}]}`))
			return
		}
		writes++
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	h := &Handler{Auth: auth(true), Service: svc.New(setupResolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "MANAGEMENT_CANARY"}}, nil)}
	listed := httptest.NewRecorder()
	h.Handle(listed, httptest.NewRequest(http.MethodGet, "/api/credential-concurrency", nil))
	var snapshot struct {
		Reference string `json:"connection_ref"`
	}
	_ = json.Unmarshal(listed.Body.Bytes(), &snapshot)
	if listed.Code != 200 || len(snapshot.Reference) != 32 || strings.Contains(listed.Body.String(), "MANAGEMENT_CANARY") || strings.Contains(listed.Body.String(), server.URL) {
		t.Fatal("快照引用回执错误")
	}
	missing := httptest.NewRecorder()
	h.Handle(missing, httptest.NewRequest(http.MethodPatch, "/api/credential-concurrency", strings.NewReader(`{"auth_id":"same","concurrency_limit":20}`)))
	if missing.Code != 400 || writes != 0 {
		t.Fatal("缺失引用仍写入")
	}
	raw, _ := json.Marshal(map[string]any{"auth_id": "same", "concurrency_limit": 20, "connection_ref": snapshot.Reference})
	saved := httptest.NewRecorder()
	h.Handle(saved, httptest.NewRequest(http.MethodPatch, "/api/credential-concurrency", strings.NewReader(string(raw))))
	if saved.Code != 200 || writes != 1 {
		t.Fatalf("引用未贯通: %d %d", saved.Code, writes)
	}
}
