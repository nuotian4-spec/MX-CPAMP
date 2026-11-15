package bps

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBPSClientInheritsDefaultTransport(t *testing.T) {
	oldDefault := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldDefault })
	for _, proxyURL := range []string{"", " \t\r\n "} {
		t.Run(map[bool]string{true: "empty", false: "whitespace"}[proxyURL == ""], func(t *testing.T) {
			calls := 0
			http.DefaultTransport = testTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				body := `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","expires_in":3600}`
				switch req.URL.String() {
				case TokenURL:
					if req.Method != http.MethodPost {
						t.Fatalf("兑换请求方法错误: %s", req.Method)
					}
				case ResetCreditsURL:
					body = `{"available_count":2}`
				case ResetCreditsURL + "/consume":
					body = `{}`
				default:
					t.Fatalf("意外请求地址: %s", req.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			client := NewClient(proxyURL)
			metadata, err := client.Exchange(t.Context(), "synthetic-code", "synthetic-verifier")
			if err != nil || metadata["access_token"] != "synthetic-access" {
				t.Fatalf("兑换失败: %v", err)
			}
			metadata, err = client.Refresh(t.Context(), metadata)
			if err != nil || metadata["refresh_token"] != "synthetic-refresh" {
				t.Fatalf("刷新失败: %v", err)
			}
			count, err := client.ResetCreditsAvailable(t.Context(), "synthetic-token", "synthetic-account")
			if err != nil || count != 2 {
				t.Fatalf("重置额度查询失败: count=%d, err=%v", count, err)
			}
			if err := client.ConsumeResetCredit(t.Context(), "synthetic-token", "synthetic-account", "synthetic-request"); err != nil {
				t.Fatalf("重置额度消费失败: %v", err)
			}
			if calls != 4 {
				t.Fatalf("默认传输调用次数 = %d，预期4", calls)
			}
		})
	}
}

func TestBPSClientExplicitTransportDoesNotFallBack(t *testing.T) {
	oldDefault := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldDefault })
	for _, tc := range []struct{ name, proxy, address string }{
		{"direct", "direct", "auth.openai.com:443"},
		{"none", "none", "auth.openai.com:443"},
		{"proxy", "http://127.0.0.1:7890", "127.0.0.1:7890"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defaultProxyCalls, dialCalls := 0, 0
			http.DefaultTransport = &http.Transport{
				Proxy: func(*http.Request) (*url.URL, error) {
					defaultProxyCalls++
					return url.Parse("http://default.invalid:1234")
				},
				DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
					dialCalls++
					if addr != tc.address {
						t.Errorf("连接目标 = %q，预期 %q", addr, tc.address)
					}
					return nil, errors.New("合成传输已阻断连接")
				},
			}
			_, err := NewClient(tc.proxy).Exchange(t.Context(), "synthetic-code", "synthetic-verifier")
			if err == nil || defaultProxyCalls != 0 || dialCalls != 1 {
				t.Fatalf("显式传输发生回退: err=%v, default=%d, dial=%d", err, defaultProxyCalls, dialCalls)
			}
		})
	}
}

func TestBPSClientInvalidProxyMakesNoRequests(t *testing.T) {
	oldDefault := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldDefault })
	calls := 0
	http.DefaultTransport = testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("意外传输请求")
	})
	client := NewClient("invalid://proxy.example")
	if _, err := client.Exchange(t.Context(), "synthetic-code", "synthetic-verifier"); err == nil {
		t.Fatal("无效代理配置仍允许兑换")
	}
	if _, err := client.Refresh(t.Context(), map[string]any{"client_id": ClientID, "refresh_token": "synthetic-refresh"}); err == nil {
		t.Fatal("无效代理配置仍允许刷新")
	}
	if _, err := client.ResetCreditsAvailable(t.Context(), "synthetic-token", "synthetic-account"); err == nil {
		t.Fatal("无效代理配置仍允许查询重置额度")
	}
	if err := client.ConsumeResetCredit(t.Context(), "synthetic-token", "synthetic-account", "synthetic-request"); err == nil {
		t.Fatal("无效代理配置仍允许消费重置额度")
	}
	if calls != 0 {
		t.Fatalf("无效代理配置发起了 %d 次请求", calls)
	}
}
