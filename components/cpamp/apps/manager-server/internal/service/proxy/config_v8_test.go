package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/managerconfig"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func TestConfigYAMLAliasUsesV8AndPreservesBody(t *testing.T) {
	const body = "config-version: 8\naccess:\n  api-keys: [synthetic]\n  client-key-scopes: [{api-key: synthetic, channel: kilo}]\nopaque: {preserve: null}\n"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v8/management/config.yaml" {
			t.Errorf("配置最终目标=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-management" {
			t.Error("未使用已存管理认证")
		}
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			if string(raw) != body || r.Header.Get("If-Match") != `"v1"` {
				t.Error("配置正文或版本条件改变")
			}
		}
		w.Header().Set("ETag", `"v2"`)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()
	service := New(managerconfig.New(config.Config{CPAUpstreamURL: upstream.URL, ManagementKey: "synthetic-management"}, nil, nil))
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req := httptest.NewRequest(method, "/v0/management/config.yaml", strings.NewReader(body))
		req.Header.Set("If-Match", `"v1"`)
		req.Header.Set("Content-Type", "application/yaml")
		rec := httptest.NewRecorder()
		service.ProxyManagement(rec, req, func(w http.ResponseWriter, status int, err error) { http.Error(w, err.Error(), status) })
		if rec.Code != 200 || rec.Body.String() != body || rec.Header().Get("ETag") != `"v2"` {
			t.Error("配置回读未完整保留")
		}
	}
	if calls != 2 {
		t.Fatalf("实际请求=%d，期望2", calls)
	}
}

type configYAMLResolver struct {
	mu    sync.Mutex
	setup store.Setup
}

func (r *configYAMLResolver) ResolveSetup(context.Context) (store.Setup, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setup, true, nil
}
func (r *configYAMLResolver) set(setup store.Setup) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setup = setup
}

func TestConfigYAMLProtectedConnectionDrift(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	writesA, writesB := 0, 0
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			_, _ = io.WriteString(w, `{"mode":"local","default_limit":20,"pool_waiting":0,"credentials":[]}`)
			return
		}
		if r.URL.Path != "/v8/management/config.yaml" {
			t.Errorf("意外目标%s", r.URL.Path)
		}
		if r.Method == http.MethodPut {
			writesA++
			close(started)
			<-release
		}
		w.Header().Set("ETag", `"a"`)
		_, _ = io.WriteString(w, "config-version: 8\naccess: {api-keys: [synthetic]}\n")
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			writesB++
		}
		w.WriteHeader(500)
	}))
	defer b.Close()
	setupA := store.Setup{CPAUpstreamURL: a.URL, ManagementKey: "synthetic-management"}
	setupB := store.Setup{CPAUpstreamURL: b.URL, ManagementKey: "synthetic-management-b"}
	resolver := &configYAMLResolver{setup: setupA}
	connections := concurrency.New(resolver, nil)
	service := New(managerconfig.New(config.Config{CPAUpstreamURL: a.URL, ManagementKey: setupA.ManagementKey}, nil, nil))
	set := func(setup store.Setup) {
		resolver.set(setup)
		service.managerConfigService = managerconfig.New(config.Config{CPAUpstreamURL: setup.CPAUpstreamURL, ManagementKey: setup.ManagementKey}, nil, nil)
	}
	request := func(method, reference string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v0/management/config.yaml", strings.NewReader("config-version: 8\n"))
		req.Header.Set(ConfigConnectionHeader, reference)
		req.Header.Set("If-Match", `"a"`)
		rec := httptest.NewRecorder()
		service.ProxyConfigYAML(rec, req, connections, func(w http.ResponseWriter, status int, err error) { http.Error(w, err.Error(), status) })
		return rec
	}
	first := request(http.MethodGet, "required")
	ref := first.Header().Get(ConfigConnectionHeader)
	if first.Code != 200 || len(ref) != 32 {
		t.Fatalf("初次配置GET未取得连接引用 status=%d", first.Code)
	}
	set(setupB)
	rejected := request(http.MethodPut, ref)
	if rejected.Code != 409 || writesB != 0 || writesA != 0 {
		t.Fatal("GET A后切B的PUT未拒绝")
	}
	set(setupA)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(http.MethodPut, ref) }()
	<-started
	resolver.set(setupB)
	close(release)
	late := <-done
	if late.Code != 409 || !strings.Contains(late.Body.String(), "状态待核") || writesA != 1 || writesB != 0 {
		t.Fatalf("迟到保存状态错误 status=%d A=%d B=%d", late.Code, writesA, writesB)
	}
	t.Logf("真实双目标：GET A后切B写B=0；PUT A在途切B回409，A写1/B写0，无重放")
}
