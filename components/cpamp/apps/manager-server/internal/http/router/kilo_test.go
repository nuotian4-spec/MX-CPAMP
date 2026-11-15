package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
	sqliterepo "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/sqlite"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/security"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/adminauth"
	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	kilosvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const kiloRouterAdminKey = "cpamp_SYNTHETIC_ADMIN"
const kiloRouterManagementKey = "SYNTHETIC_MANAGEMENT"
const kiloRouterConcurrencyFixture = `{"mode":"local","default_limit":20,"pool_waiting":0,"credentials":[]}`

type kiloRouterResolver struct {
	mu    sync.Mutex
	setup store.Setup
}

func (r *kiloRouterResolver) ResolveSetup(context.Context) (store.Setup, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setup, r.setup.CPAUpstreamURL != "", nil
}

func (r *kiloRouterResolver) replace(setup store.Setup) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setup = setup
}

func newKiloFixtureRouter(t *testing.T, resolver *kiloRouterResolver, client *http.Client) http.Handler {
	t.Helper()
	// 仅在测试进程内初始化现有表结构，所有认证值都是合成数据。
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := sqliterepo.Migrate(db); err != nil {
		t.Fatalf("初始化内存测试库: %v", err)
	}
	st := store.New(db)
	credential, err := security.NewAdminCredential(kiloRouterAdminKey, "kilo-router-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAdminCredential(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{CORSOrigins: []string{"http://fixture.invalid"}}
	connections := concurrency.New(resolver, nil)
	return New(&app.Context{
		Config:                       cfg,
		Store:                        st,
		AdminAuthService:             adminauth.New(cfg, st),
		CredentialConcurrencyService: connections,
		KiloService:                  kilosvc.New(resolver, connections, client),
	})
}

func kiloRouterRequest(handler http.Handler, method, path, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Origin", "http://fixture.invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertKiloRouterSanitized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, secret := range []string{"SECRET", "UPSTREAM_REF", kiloRouterAdminKey, kiloRouterManagementKey, "api_key", "proxy_url", `"token"`} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("响应泄露字段 %s: %s", secret, rec.Body.String())
		}
	}
}

func TestKiloRouterAuthenticatesAndReusesConnectionReference(t *testing.T) {
	var connectionCalls, kiloCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey || r.URL.RawQuery != "" {
			t.Error("未使用只读管理认证请求")
		}
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			connectionCalls.Add(1)
			_, _ = w.Write([]byte(kiloRouterConcurrencyFixture))
		case "/v8/management/kilo/accounts":
			kiloCalls.Add(1)
			_, _ = w.Write([]byte(`{"accounts":[{"id":"fixture","name":"合成账号","enabled":true,"effective_limit":20,"quota_source":"none","token":"SECRET_TOKEN","api_key":"SECRET_KEY","proxy_url":"http://SECRET_PROXY"}],"connection_ref":"UPSTREAM_REF"}`))
		case "/v8/management/kilo/status":
			kiloCalls.Add(1)
			_, _ = w.Write([]byte(`{"configured":true,"account_count":1,"token":"SECRET_STATUS"}`))
		case "/v8/management/kilo/capabilities":
			kiloCalls.Add(1)
			_, _ = w.Write([]byte(`{"channel":"kilo","public_protocols":["responses","chat_completions"],"tools_semantics":"source_parity","images_semantics":"source_parity","token":"SECRET_CAPABILITIES"}`))
		default:
			t.Errorf("意外的上游路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}
	handler := newKiloFixtureRouter(t, resolver, upstream.Client())
	var reference string
	for i, resource := range []string{"accounts", "status", "capabilities"} {
		path := "/api/kilo/" + resource
		for _, key := range []string{"", "SYNTHETIC_CLIENT_KEY", kiloRouterManagementKey} {
			rec := kiloRouterRequest(handler, http.MethodGet, path, key)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("管理鉴权门禁失效: %s %d %s", path, rec.Code, rec.Body.String())
			}
		}
		if connectionCalls.Load() != int32(i) || kiloCalls.Load() != int32(i) {
			t.Fatal("未授权请求触达上游")
		}
		rec := kiloRouterRequest(handler, http.MethodGet, path, kiloRouterAdminKey)
		if rec.Code != http.StatusOK {
			t.Fatalf("真实路由读取失败: %s %d %s", path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Access-Control-Allow-Origin") != "http://fixture.invalid" {
			t.Fatal("缓存或跨域包装未装配")
		}
		var result struct {
			ConnectionRef string `json:"connection_ref"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.ConnectionRef) != 32 {
			t.Fatalf("缺少服务端引用: %s", rec.Body.String())
		}
		if i == 0 {
			reference = result.ConnectionRef
		} else if reference != result.ConnectionRef {
			t.Fatal("相同连接引用未复用")
		}
		assertKiloRouterSanitized(t, rec)
		if connectionCalls.Load() != int32(i+1) || kiloCalls.Load() != int32(i+1) {
			t.Fatal("一次读取产生了重复快照或Kilo请求")
		}
		post := kiloRouterRequest(handler, http.MethodPost, path, kiloRouterAdminKey)
		wantPost := http.StatusMethodNotAllowed
		if resource == "accounts" {
			wantPost = http.StatusBadRequest
		}
		if post.Code != wantPost {
			t.Fatalf("方法或写入参数限制失效: %d", post.Code)
		}
	}
	// 先成功读Kilo，再访问既有并发路由，证明无需预热且共享同一引用。
	rec := kiloRouterRequest(handler, http.MethodGet, "/api/credential-concurrency", kiloRouterAdminKey)
	var snapshot concurrency.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || snapshot.ConnectionRef != reference || connectionCalls.Load() != 4 || kiloCalls.Load() != 3 {
		t.Fatalf("既有并发路由未共享引用: %d %+v", rec.Code, snapshot)
	}
}

func TestKiloRouterMissingSetup(t *testing.T) {
	handler := newKiloFixtureRouter(t, &kiloRouterResolver{}, nil)
	for _, resource := range []string{"accounts", "status", "capabilities"} {
		rec := kiloRouterRequest(handler, http.MethodGet, "/api/kilo/"+resource, kiloRouterAdminKey)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "cpa_unavailable") {
			t.Fatalf("未配置分类错误: %d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestKiloRouterRejectsConnectionDriftWithoutRetry(t *testing.T) {
	for _, phase := range []string{"reference", "kilo"} {
		for _, change := range []string{"url", "key", "removed"} {
			t.Run(phase+"_"+change, func(t *testing.T) {
				resolver := &kiloRouterResolver{}
				var connectionCalls, kiloCalls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					currentPhase := "kilo"
					body := `{"configured":true}`
					switch r.URL.Path {
					case "/v8/management/credentials/concurrency":
						currentPhase = "reference"
						connectionCalls.Add(1)
						body = kiloRouterConcurrencyFixture
					case "/v8/management/kilo/status":
						kiloCalls.Add(1)
					default:
						t.Errorf("意外路径: %s", r.URL.Path)
					}
					if currentPhase == phase {
						setup, _, _ := resolver.ResolveSetup(context.Background())
						switch change {
						case "url":
							setup.CPAUpstreamURL = "http://never-contact.invalid"
						case "key":
							setup.ManagementKey = "CHANGED"
						case "removed":
							setup = store.Setup{}
						}
						resolver.replace(setup)
					}
					_, _ = w.Write([]byte(body))
				}))
				defer upstream.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey})
				handler := newKiloFixtureRouter(t, resolver, upstream.Client())
				rec := kiloRouterRequest(handler, http.MethodGet, "/api/kilo/status", kiloRouterAdminKey)
				wantKiloCalls := int32(1)
				if phase == "reference" {
					wantKiloCalls = 0
				}
				if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "cpa_target_changed") || connectionCalls.Load() != 1 || kiloCalls.Load() != wantKiloCalls {
					t.Fatalf("漂移未拒绝或发生重试: %d %s concurrency=%d kilo=%d", rec.Code, rec.Body.String(), connectionCalls.Load(), kiloCalls.Load())
				}
			})
		}
	}
}

func TestKiloRouterSanitizesUpstreamErrors(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					_, _ = w.Write([]byte(kiloRouterConcurrencyFixture))
					return
				}
				w.WriteHeader(code)
				_, _ = w.Write([]byte("SECRET_TOKEN SECRET_PROXY " + kiloRouterManagementKey))
			}))
			defer upstream.Close()
			resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}
			handler := newKiloFixtureRouter(t, resolver, upstream.Client())
			rec := kiloRouterRequest(handler, http.MethodGet, "/api/kilo/status", kiloRouterAdminKey)
			wantCode := "kilo_read_failed"
			if code < 500 {
				wantCode = "kilo_management_auth_failed"
			}
			if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), wantCode) {
				t.Fatalf("上游错误分类错误: %d %s", rec.Code, rec.Body.String())
			}
			assertKiloRouterSanitized(t, rec)
		})
	}
}
