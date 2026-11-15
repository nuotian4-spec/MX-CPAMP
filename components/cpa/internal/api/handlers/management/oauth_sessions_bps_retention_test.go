package management

import (
	"testing"
	"time"
)

func TestBPSNativeCompletionRetention(t *testing.T) {
	cases := []struct {
		name           string
		provider       string
		source         string
		ttl            time.Duration
		retainOriginal bool
	}{
		{name: "native_bps", provider: "bps", source: oauthSessionSourceBuiltin, ttl: oauthSessionTTL, retainOriginal: true},
		{name: "native_bps_short_ttl", provider: "bps", source: oauthSessionSourceBuiltin, ttl: 30 * time.Second, retainOriginal: true},
		{name: "builtin_codex", provider: "codex", source: oauthSessionSourceBuiltin, ttl: oauthSessionTTL},
		{name: "plugin_bps", provider: "bps", source: oauthSessionSourcePlugin, ttl: oauthSessionTTL},
		{name: "builtin_cpa_bps", provider: "cpa-bps", source: oauthSessionSourceBuiltin, ttl: oauthSessionTTL},
	}

	for _, completionMode := range []string{"outcome", "provider"} {
		t.Run(completionMode, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					store := newOAuthSessionStore(tc.ttl)
					const state = "bps-retention-state"
					if tc.source == oauthSessionSourcePlugin {
						if err := store.RegisterPlugin(state, tc.provider, nil); err != nil {
							t.Fatalf("注册插件会话失败: %v", err)
						}
					} else {
						store.Register(state, tc.provider)
					}
					if tc.ttl < oauthCompletedSessionTTL {
						store.mu.Lock()
						session := store.sessions[state]
						session.CreatedAt = time.Now().Add(-10 * time.Second)
						session.ExpiresAt = session.CreatedAt.Add(tc.ttl)
						store.sessions[state] = session
						store.mu.Unlock()
					}
					original, ok := store.Get(state)
					if !ok {
						t.Fatal("注册后的会话缺失")
					}
					if !original.ExpiresAt.Equal(original.CreatedAt.Add(tc.ttl)) {
						t.Fatal("会话原始期限与创建时间及配置 TTL 不一致")
					}

					complete := func(wantNew int) {
						t.Helper()
						if completionMode == "provider" {
							if got := store.CompleteProvider(tc.provider, tc.source); got != wantNew {
								t.Fatalf("批量完成数量 = %d，期望 %d", got, wantNew)
							}
							return
						}
						store.CompleteWithOutcome(state, "retained-auth", "retained-grant", true, true, true, 1, 2, 3, 4, "", "")
					}
					beforeCompletion := time.Now()
					complete(1)
					afterCompletion := time.Now()
					completed, ok := store.Get(state)
					if !ok || !completed.Completed {
						t.Fatalf("完成后的会话存在/完成 = %t/%t，期望 true/true", ok, completed.Completed)
					}
					if completionMode == "outcome" && (completed.AuthID != "retained-auth" || !completed.Saved || !completed.Adopted) {
						t.Fatalf("完成结果未保留: authID=%q saved=%t adopted=%t", completed.AuthID, completed.Saved, completed.Adopted)
					}
					if tc.retainOriginal {
						if !completed.ExpiresAt.Equal(original.ExpiresAt) {
							t.Errorf("原生 BPS 完成后期限 = %s，期望保留创建时期限 %s", completed.ExpiresAt, original.ExpiresAt)
						}
					} else {
						if completed.ExpiresAt.Before(beforeCompletion.Add(time.Minute)) || completed.ExpiresAt.After(afterCompletion.Add(time.Minute)) {
							t.Errorf("非原生 BPS 完成后期限 = %s，期望完成后 1 分钟", completed.ExpiresAt)
						}
					}

					for read := 0; read < 3; read++ {
						readBack, exists := store.Get(state)
						if !exists || !readBack.ExpiresAt.Equal(completed.ExpiresAt) {
							t.Fatalf("第 %d 次读取改变会话期限或丢失会话", read+1)
						}
					}
					complete(0)
					repeated, ok := store.Get(state)
					if !ok || !repeated.ExpiresAt.Equal(completed.ExpiresAt) {
						t.Fatal("重复完成改变会话期限或丢失会话")
					}

					if tc.retainOriginal && tc.ttl == oauthSessionTTL {
						store.mu.Lock()
						store.purgeExpiredLocked(original.CreatedAt.Add(25 * time.Minute))
						store.mu.Unlock()
						atManagerDeadline, exists := store.Get(state)
						if !exists || !atManagerDeadline.Completed {
							t.Error("创建后 25 分钟完成结果已被清理，未覆盖 Manager 轮询窗口")
						}
					}
					expiresAt := completed.ExpiresAt
					if tc.retainOriginal {
						expiresAt = original.ExpiresAt
					}
					store.mu.Lock()
					store.purgeExpiredLocked(expiresAt.Add(time.Nanosecond))
					store.mu.Unlock()
					if _, exists := store.Get(state); exists {
						t.Error("截止时间之后会话仍然存在")
					}
				})
			}
		})
	}
}
