package oauthlogin

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type statusSerialResolver func(context.Context) (store.Setup, bool, error)

func (f statusSerialResolver) ResolveSetup(ctx context.Context) (store.Setup, bool, error) {
	return f(ctx)
}

func TestNativeBPSStatusSerialCache(t *testing.T) {
	for _, tc := range []struct {
		name, lateResponse string
	}{
		{"late_wait", `{"status":"wait"}`},
		{"late_missing", `{"status":"error","error":"unknown or expired state"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}
			baseResolver := &resolver{setup: setup}
			secondCtx, cancelSecond := context.WithCancel(context.Background())
			defer cancelSecond()
			secondEntered := make(chan struct{})
			var signalSecond sync.Once
			s := New(statusSerialResolver(func(ctx context.Context) (store.Setup, bool, error) {
				resolved, ok, err := baseResolver.ResolveSetup(ctx)
				if ctx == secondCtx {
					signalSecond.Do(func() { close(secondEntered) })
				}
				return resolved, ok, err
			}), nil, "", "")
			session := &oauthSession{provider: "bps", setup: setup, created: time.Now(), submitted: true}
			s.sessions["state"] = session
			firstEntered, releaseFirst := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblockFirst := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
			defer unblockFirst()
			var calls atomic.Int32
			s.client.Transport = sessionContractTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/v8/management/oauth/status" || r.URL.Query().Get("state") != "state" || r.URL.Query().Get("provider") != "bps" {
					t.Errorf("状态查询路径或会话归属错误: %s %s", r.Method, r.URL.String())
				}
				if calls.Add(1) == 1 {
					close(firstEntered)
					<-releaseFirst
					return sessionContractResponse(`{"status":"ok","saved":true,"adopted":true,"auth_id":"synthetic-auth"}`), nil
				}
				return sessionContractResponse(tc.lateResponse), nil
			})
			type result struct {
				out map[string]any
				err error
			}
			query := func(ctx context.Context, done chan<- result) {
				out, err := s.OAuth(ctx, http.MethodGet, "status", map[string]string{"state": "state"})
				done <- result{out: out, err: err}
			}
			firstDone, secondDone := make(chan result, 1), make(chan result, 1)
			go query(context.Background(), firstDone)
			<-firstEntered
			if session.statusMu.TryLock() {
				session.statusMu.Unlock()
				t.Error("首个 status 等待上游期间未持有会话串行锁")
			}
			if s.mu.TryLock() {
				s.mu.Unlock()
			} else {
				t.Error("首个 status 等待上游期间持有服务全局锁")
			}
			go query(secondCtx, secondDone)
			<-secondEntered
			unblockFirst()
			for index, got := range []result{<-firstDone, <-secondDone} {
				if got.err != nil || got.out["status"] != "ok" || got.out["saved"] != true || got.out["adopted"] != true || got.out["auth_id"] != "synthetic-auth" {
					t.Fatalf("第 %d 个 status 未保留真实完成结果: out=%v err=%v", index+1, got.out, got.err)
				}
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("同会话第二个 status 未复用完成缓存，上游调用 = %d，期望 1", got)
			}
		})
	}
}

func TestNativeBPSStatusTerminalRecheck(t *testing.T) {
	for _, tc := range []struct {
		name, firstResponse, firstCode, firstOutcome string
		wantCalls                                    int32
	}{
		{"missing_can_recheck", `{"status":"error","error":"unknown or expired state"}`, "session_missing_or_expired", "unknown", 2},
		{"exchange_failure_is_terminal", `{"status":"error","error":"oauth_exchange_failed","reason":"invalid_grant","save_outcome":"not_saved"}`, "oauth_exchange_failed", "not_saved", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := store.Setup{CPAUpstreamURL: "http://fixture.invalid", ManagementKey: "synthetic"}
			s := New(&resolver{setup: setup}, nil, "", "")
			s.sessions["state"] = &oauthSession{provider: "bps", setup: setup, created: time.Now(), submitted: true}
			var calls atomic.Int32
			s.client.Transport = sessionContractTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/v8/management/oauth/status" || r.URL.Query().Get("state") != "state" {
					t.Errorf("重新核对改变了方法、路径或 state: %s %s", r.Method, r.URL.String())
				}
				if calls.Add(1) == 1 {
					return sessionContractResponse(tc.firstResponse), nil
				}
				return sessionContractResponse(`{"status":"ok","saved":true,"adopted":true,"auth_id":"synthetic-auth"}`), nil
			})
			first, err := s.OAuth(context.Background(), http.MethodGet, "status", map[string]string{"state": "state"})
			if err != nil || first["status"] != "error" || first["error_code"] != tc.firstCode || first["save_outcome"] != tc.firstOutcome {
				t.Fatalf("首次诊断结果错误: out=%v err=%v", first, err)
			}
			for read := 0; read < 2; read++ {
				out, err := s.OAuth(context.Background(), http.MethodGet, "status", map[string]string{"state": "state"})
				if err != nil {
					t.Fatalf("同会话第 %d 次重新核对失败: %v", read+1, err)
				}
				if tc.wantCalls == 2 {
					if out["status"] != "ok" || out["saved"] != true || out["adopted"] != true || out["auth_id"] != "synthetic-auth" {
						t.Fatalf("missing 后未通过同会话 GET 恢复真实完成结果: %v", out)
					}
				} else if out["status"] != "error" || out["error_code"] != "oauth_exchange_failed" || out["save_outcome"] != "not_saved" || out["reason"] != "invalid_grant" {
					t.Fatalf("真实兑换失败终态被覆盖: %v", out)
				}
			}
			if got := calls.Load(); got != tc.wantCalls {
				t.Fatalf("上游调用 = %d，期望 %d", got, tc.wantCalls)
			}
		})
	}
}
