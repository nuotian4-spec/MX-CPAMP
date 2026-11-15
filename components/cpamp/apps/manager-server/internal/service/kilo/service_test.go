package kilo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type setupResolver struct {
	mu    sync.Mutex
	value store.Setup
	ok    bool
}

func (r *setupResolver) ResolveSetup(context.Context) (store.Setup, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value, r.ok, nil
}
func (r *setupResolver) replace(value store.Setup) { r.mu.Lock(); defer r.mu.Unlock(); r.value = value }

type connectionFunc func(context.Context) (concurrency.Snapshot, error)

func (f connectionFunc) List(ctx context.Context) (concurrency.Snapshot, error) { return f(ctx) }

func TestReadUsesExistingReferenceWithoutClientPrerequisite(t *testing.T) {
	kiloCalls := 0
	connectionCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer MANAGEMENT_FIXTURE" {
			t.Error("未使用保存的管理认证")
		}
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			connectionCalls++
			_, _ = w.Write([]byte(`{"mode":"local","default_limit":20,"pool_waiting":0,"credentials":[]}`))
		case "/v8/management/kilo/accounts":
			kiloCalls++
			_, _ = w.Write([]byte(`{"accounts":[],"token":"SECRET","connection_ref":"UPSTREAM_FAKE"}`))
		case "/v8/management/kilo/status":
			kiloCalls++
			_, _ = w.Write([]byte(`{"configured":false,"account_count":0,"healthy_proxy_count":0,"busy_proxy_count":0}`))
		case "/v8/management/kilo/capabilities":
			kiloCalls++
			_, _ = w.Write([]byte(`{"channel":"kilo","public_protocols":["responses"],"tools_semantics":"source_parity","images_semantics":"source_parity"}`))
		default:
			t.Error("意外请求")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	resolver := &setupResolver{value: store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "MANAGEMENT_FIXTURE"}, ok: true}
	connections := concurrency.New(resolver, nil)
	service := New(resolver, connections, server.Client())
	accounts, err := service.Accounts(context.Background())
	if err != nil || len(accounts.ConnectionRef) != 32 || accounts.Accounts == nil {
		t.Fatalf("无前置条件读取失败: %+v %v", accounts, err)
	}
	status, err := service.Status(context.Background())
	if err != nil || status.ConnectionRef != accounts.ConnectionRef || status.AutoAccountProbeEnabled || status.AutoModelProbeEnabled {
		t.Fatalf("状态读取错误: %+v %v", status, err)
	}
	capabilities, err := service.Capabilities(context.Background())
	if err != nil || capabilities.ConnectionRef != accounts.ConnectionRef || capabilities.UpstreamBillingVerified {
		t.Fatalf("能力读取错误: %+v %v", capabilities, err)
	}
	for _, result := range []any{accounts, status, capabilities} {
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "UPSTREAM_FAKE") || strings.Contains(string(raw), "MANAGEMENT_FIXTURE") {
			t.Fatal("泄露敏感字段或信任上游引用")
		}
	}
	if kiloCalls != 3 || connectionCalls != 3 {
		t.Fatalf("每次读取应仅取一次快照和Kilo数据: kilo=%d concurrency=%d", kiloCalls, connectionCalls)
	}
}

func TestMissingSetupAndConnectionFailure(t *testing.T) {
	for _, service := range []*Service{New(nil, nil, nil), New(&setupResolver{}, connectionFunc(func(context.Context) (concurrency.Snapshot, error) {
		t.Fatal("未配置仍读取上游")
		return concurrency.Snapshot{}, nil
	}), nil), New(&setupResolver{ok: true, value: store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "FIXTURE"}}, connectionFunc(func(context.Context) (concurrency.Snapshot, error) {
		return concurrency.Snapshot{}, errors.New("SECRET_CONNECTION")
	}), nil)} {
		_, err := service.Status(context.Background())
		status, _, message := HTTPError(err)
		if status != 503 || strings.Contains(message, "SECRET") {
			t.Fatalf("未配置分类或脱敏错误: %v", err)
		}
	}
}

func TestConnectionDriftRejectsAndDoesNotRetryKilo(t *testing.T) {
	for _, change := range []string{"url", "key", "removed"} {
		t.Run(change, func(t *testing.T) {
			resolver := &setupResolver{ok: true}
			calls := 0
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				current, _, _ := resolver.ResolveSetup(context.Background())
				switch change {
				case "url":
					current.CPAUpstreamURL = "http://never-contact.invalid"
				case "key":
					current.ManagementKey = "CHANGED"
				}
				resolver.replace(current)
				if change == "removed" {
					resolver.replace(store.Setup{})
				}
				_, _ = w.Write([]byte(`{"configured":true}`))
			}))
			defer server.Close()
			resolver.replace(store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "FIXTURE"})
			connections := connectionFunc(func(context.Context) (concurrency.Snapshot, error) {
				reads++
				ref := strings.Repeat("a", 32)
				return concurrency.Snapshot{Mode: "local", ConnectionRef: ref}, nil
			})
			_, err := New(resolver, connections, server.Client()).Status(context.Background())
			status, code, _ := HTTPError(err)
			if status != 409 || code != "cpa_target_changed" || calls != 1 || reads != 1 {
				t.Fatalf("连接漂移未拒绝或发生重复读取: %v kilo=%d concurrency=%d", err, calls, reads)
			}
		})
	}
}

func TestUpstreamErrorsAreSanitized(t *testing.T) {
	for _, code := range []int{401, 403, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte("SECRET_TOKEN SECRET_PROXY"))
		}))
		resolver := &setupResolver{ok: true, value: store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "FIXTURE"}}
		connections := connectionFunc(func(context.Context) (concurrency.Snapshot, error) {
			return concurrency.Snapshot{Mode: "local", ConnectionRef: strings.Repeat("a", 32)}, nil
		})
		_, err := New(resolver, connections, server.Client()).Status(context.Background())
		status, errorCode, message := HTTPError(err)
		server.Close()
		if status != 502 || strings.Contains(message, "SECRET") {
			t.Fatalf("错误泄露: %v", err)
		}
		if code < 500 && errorCode != "kilo_management_auth_failed" {
			t.Fatal("认证失败未分类")
		}
	}
}
