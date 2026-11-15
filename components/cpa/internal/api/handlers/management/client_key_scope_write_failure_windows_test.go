package management

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientKeyScopeWriteFailureDoesNotPublish(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := []byte("config-version: 8\naccess: {api-keys: [old]}\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	h := &Handler{cfg: cfg, configFilePath: path}
	published := make(chan bool, 1)
	h.SetConfigReloadHook(func(_ context.Context, _ *config.Config) { published <- true })
	router := gin.New()
	router.PATCH("/config", h.ConfigV8)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("PATCH", "/config", strings.NewReader(`{"access":{"api-keys":["old","new"],"client-key-scopes":[{"api-key":"new","channel":"bps"}]}}`)))
	if rec.Code != 500 || h.cfg != cfg {
		t.Fatal("写盘失败仍发布内存", rec.Code, rec.Body.String())
	}
	if data, e := os.ReadFile(path); e != nil || string(data) != string(raw) {
		t.Fatal("失败修改原配置", e)
	}
	select {
	case <-published:
		t.Fatal("失败触发运行时重载")
	default:
	}
}
