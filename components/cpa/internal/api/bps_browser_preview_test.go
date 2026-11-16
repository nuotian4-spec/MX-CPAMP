package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/buildinfo"
)

type bpsPreviewCapabilityWriter struct {
	http.ResponseWriter
}

func (w bpsPreviewCapabilityWriter) WriteHeader(status int) {
	if w.Header().Get("X-CPA-SUPPORT-PLUGIN") != "" {
		w.Header().Set("X-CPA-SUPPORT-PLUGIN", "1")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w bpsPreviewCapabilityWriter) Write(body []byte) (int, error) {
	if w.Header().Get("X-CPA-SUPPORT-PLUGIN") != "" {
		w.Header().Set("X-CPA-SUPPORT-PLUGIN", "1")
	}
	return w.ResponseWriter.Write(body)
}

func bpsPreviewPlatformHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(bpsPreviewCapabilityWriter{ResponseWriter: w}, r)
	})
}

func TestBPSManagementBrowserPreview(t *testing.T) {
	if os.Getenv("BPS_PREVIEW_PANEL") == "" || os.Getenv("BPS_PREVIEW_RECEIPT") == "" {
		t.Skip("仅在最终 Web 产物生成后显式启动隔离预览")
	}
	previousVersion := buildinfo.Version
	buildinfo.Version = "v0.0.0-bps-local-preview"
	t.Cleanup(func() { buildinfo.Version = previousVersion })
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("CPA 预览拒绝非 loopback 连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport; transport.CloseIdleConnections() })
	server, upstream, fixture := newBPSManagementRouter(t, true)
	unregistered, missing, _ := newBPSManagementRouter(t, false)
	upstream.Close()
	missing.Close()
	upstream = httptest.NewServer(bpsPreviewPlatformHeader(server.Handler()))
	missing = httptest.NewServer(bpsPreviewPlatformHeader(unregistered.Handler()))
	defer upstream.Close()
	defer missing.Close()
	stop := make(chan struct{})
	var once sync.Once
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wait-stop":
			select {
			case <-stop:
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
			}
		case r.Method == http.MethodPost && r.URL.Path == "/stop":
			once.Do(func() { close(stop) })
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/scenario":
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			fixture.status, fixture.active, fixture.required = 0, false, true
			switch r.URL.Query().Get("name") {
			case "inactive":
				fixture.code = "not_activated"
			case "active":
				fixture.active, fixture.code = true, "active"
			case "not_required":
				fixture.required, fixture.code = false, "not_required"
			case "unknown":
				fixture.code = "fixture_unknown_vendor_code"
			case "expired":
				fixture.code = "license_expired"
			case "http404":
				fixture.status, fixture.code = http.StatusNotFound, "fixture_not_found"
			case "http500":
				fixture.status, fixture.code = http.StatusInternalServerError, "fixture_upstream_error"
			default:
				http.Error(w, "未知合成状态", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/state":
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			doc := ojson.NewDoc([]byte(`{}`))
			for name, value := range map[string]any{"reads": fixture.reads, "activates": fixture.activates, "refreshes": fixture.refreshes, "reloads": fixture.reloads, "active": fixture.active, "code": fixture.code} {
				if err := doc.Set(name, value); err != nil {
					http.Error(w, "状态编码失败", http.StatusInternalServerError)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(doc.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer control.Close()
	bpsRunManagerTest(t, upstream.URL, missing.URL, "TestBPSManagerBrowserPreview", "BPS_PREVIEW_CONTROL_URL="+control.URL)
}
