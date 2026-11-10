package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestBPSSettingsSavesFreshSnapshotAndRejectsRelay(t *testing.T) {
	h, path, _ := newIssue6631Handler(t, "auth-dir: auths\n")
	original := h.configSnapshot()
	var applied *config.Config
	h.SetConfigReloadHook(func(_ context.Context, cfg *config.Config) { applied = cfg })
	router := gin.New()
	router.PUT("/settings", h.PutBPSSettings)
	router.GET("/settings", h.GetBPSSettings)
	for _, raw := range []string{`{"enabled":true,"images_enabled":true,"image_transport":"relay","image_relay_base_url":""}`, `{"enabled":true,"images_enabled":true,"image_transport":"native","image_relay_base_url":"","admin_secret":"CANARY"}`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(raw)))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	putIssue6631Value(t, router, "/settings", `{"enabled":false,"images_enabled":false,"image_transport":"native","image_relay_base_url":""}`)
	if applied == nil || applied.IsBPSProtocolEnabled() || applied.IsBPSImagesEnabled() {
		t.Fatal("保存未同步应用新快照")
	}
	if !original.IsBPSProtocolEnabled() || !original.IsBPSImagesEnabled() {
		t.Fatal("保存改变在途快照")
	}
	if readIssue6631Disk(t, path).IsBPSImagesEnabled() {
		t.Fatal("磁盘未保存关闭状态")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ttl_seconds":1800`) || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestBPSSettingsConcurrentReadsKeepSnapshots(t *testing.T) {
	h, _, _ := newIssue6631Handler(t, "auth-dir: auths\n")
	router := gin.New()
	router.PUT("/settings", h.PutBPSSettings)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				cfg := h.configSnapshot()
				_ = cfg.IsBPSImagesEnabled()
				_ = cfg.IsBPSProtocolEnabled()
			}
		}
	}()
	for i := 0; i < 6; i++ {
		putIssue6631Value(t, router, "/settings", `{"enabled":true,"images_enabled":false,"image_transport":"native","image_relay_base_url":""}`)
	}
	close(stop)
	wg.Wait()
}
