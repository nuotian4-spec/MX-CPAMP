package credentialconcurrency

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func TestBPSSettingsConnectionFenceAndSecretWhitelist(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v8/management/bps/settings" || r.Header.Get("Authorization") != "Bearer ADMIN_CANARY" {
			t.Error("上游路由或认证错误")
		}
		if r.Method == http.MethodGet {
			reply(w, map[string]any{"enabled": true, "images_enabled": true, "image_transport": "native", "image_relay_base_url": "", "limits": map[string]int{"ttl_seconds": 1800}, "access_token": "TOKEN_CANARY", "admin_secret": "ADMIN_CANARY"})
			return
		}
		writes++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 4 || body["connection_ref"] != nil {
			t.Error("上游写入字段扩大")
		}
		reply(w, map[string]string{"status": "saved"})
	}))
	defer server.Close()
	r := &resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "ADMIN_CANARY"}}
	s := New(r, nil)
	ctx := context.Background()
	snapshot, err := s.GetBPSSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "CANARY") || strings.Contains(string(raw), server.URL) {
		t.Fatal("设置回执泄漏凭据或内部地址")
	}
	enabled := true
	images := false
	input := BPSSettingsInput{Enabled: &enabled, ImagesEnabled: &images, ImageTransport: "native", ConnectionRef: snapshot.ConnectionRef}
	if err = s.PutBPSSettings(ctx, input); err != nil || writes != 1 {
		t.Fatal(err, writes)
	}
	r.value.ManagementKey = "changed"
	if err = s.PutBPSSettings(ctx, input); err == nil || writes != 1 {
		t.Fatal("旧页面在新连接上仍产生写入", writes)
	}
}

func TestBPSSettingsInvalidRelayNeverWrites(t *testing.T) {
	s := New(&resolver{}, nil)
	enabled := true
	for _, base := range []string{"", "http://example.com", "https://example.com/path", "https://user:pass@example.com"} {
		if err := s.PutBPSSettings(context.Background(), BPSSettingsInput{Enabled: &enabled, ImagesEnabled: &enabled, ImageTransport: "relay", ImageRelayBaseURL: base}); err == nil {
			t.Fatal("接受无效中转基址")
		}
	}
}
