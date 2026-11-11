package credentialconcurrency

import (
	"context"
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestBPSModelsOnlyWritesFieldWithRevision(t *testing.T) {
	writes := 0
	models := []string{"old"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("ETag", `"v1"`)
			reply(w, map[string]any{"oauth": map[string]any{"providers": map[string]any{"bps": map[string]any{"models": models}}}, "other": "preserved"})
			return
		}
		writes++
		if r.Method != "PUT" || r.URL.Path != "/v8/management/config/oauth/providers/bps/models" || r.Header.Get("If-Match") != `"v1"` {
			t.Error("写入越界或丢失版本")
		}
		if json.NewDecoder(r.Body).Decode(&models) != nil {
			t.Error("非模型数组")
		}
		reply(w, map[string]string{"status": "ok"})
	}))
	defer server.Close()
	r := &resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "test"}}
	s := New(r, nil)
	ctx := context.Background()
	snapshot, err := s.GetBPSModels(ctx)
	if err != nil || !reflect.DeepEqual(snapshot.Models, models) {
		t.Fatal(snapshot, err)
	}
	if writes != 0 {
		t.Fatal("读取提前保存")
	}
	snapshot.Models = []string{"new", "new"}
	if err = s.PutBPSModels(ctx, snapshot); err != nil || !reflect.DeepEqual(models, []string{"new"}) {
		t.Fatal(models, err)
	}
	snapshot.Models = []string{}
	if err = s.PutBPSModels(ctx, snapshot); err != nil || len(models) != 0 {
		t.Fatal(models, err)
	}
	r.value.CPAUpstreamURL = "http://changed.invalid"
	if err = s.PutBPSModels(ctx, snapshot); err == nil || writes != 2 {
		t.Fatal("连接切换后发生保存", writes)
	}
}
