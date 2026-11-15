package cpa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const kiloAccountFixture = `{"accounts":[{"id":"kilo-fixture","name":"合成账号","enabled":true,"priority":0,"organization_id":"org-fixture","allow_direct_when_proxy_full":false,"concurrency_limit":null,"effective_limit":20,"active":1,"waiting":0,"quota_source":"codex_ref","codex_credential_id":"codex-fixture","token":"SECRET_TOKEN","api_key":"SECRET_KEY","proxy_url":"http://user:SECRET_PROXY@invalid"}],"token":"SECRET_OUTER"}`
const kiloCapabilitiesFixture = `{"channel":"kilo","public_protocols":["responses"],"tools_semantics":"source_parity","images_semantics":"source_parity","auto_account_probe_default":false,"auto_model_probe_default":false,"upstream_billing_verified":false,"api_key":"SECRET_CAPABILITY"}`

func TestKiloReadsWhitelistAndManagementAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer MANAGEMENT_FIXTURE" || r.URL.RawQuery != "" {
			t.Error("未使用固定只读管理请求")
		}
		switch r.URL.Path {
		case "/v8/management/kilo/accounts":
			_, _ = w.Write([]byte(kiloAccountFixture))
		case "/v8/management/kilo/status":
			_, _ = w.Write([]byte(`{"configured":false,"account_count":0,"healthy_proxy_count":0,"busy_proxy_count":0,"token":"SECRET_STATUS"}`))
		case "/v8/management/kilo/capabilities":
			_, _ = w.Write([]byte(kiloCapabilitiesFixture))
		default:
			t.Error("访问了契约外路径")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	accounts, err := FetchKiloAccounts(ctx, server.Client(), server.URL, "MANAGEMENT_FIXTURE")
	if err != nil || len(accounts.Accounts) != 1 || accounts.Accounts[0].EffectiveLimit != 20 || accounts.Accounts[0].CodexCredentialID != "codex-fixture" {
		t.Fatalf("账号回执错误: %+v %v", accounts, err)
	}
	status, err := FetchKiloStatus(ctx, server.Client(), server.URL, "MANAGEMENT_FIXTURE")
	if err != nil || status.AutoAccountProbeEnabled || status.AutoModelProbeEnabled {
		t.Fatalf("探测默认值错误: %+v %v", status, err)
	}
	capabilities, err := FetchKiloCapabilities(ctx, server.Client(), server.URL, "MANAGEMENT_FIXTURE")
	if err != nil || capabilities.UpstreamBillingVerified || capabilities.AutoAccountProbeDefault || capabilities.AutoModelProbeDefault {
		t.Fatalf("能力回执错误: %+v %v", capabilities, err)
	}
	for _, value := range []any{accounts, status, capabilities} {
		raw, _ := json.Marshal(value)
		for _, forbidden := range []string{"SECRET", "api_key", "proxy_url", "token"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("回执泄露字段 %s", forbidden)
			}
		}
	}
}

func TestKiloErrorsDiscardUpstreamBodyAndRedirects(t *testing.T) {
	for _, code := range []int{301, 401, 403, 429, 500, 502} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			redirectCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect" {
					redirectCalls++
					w.WriteHeader(200)
					return
				}
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(code)
				_, _ = w.Write([]byte("SECRET_TOKEN MANAGEMENT_FIXTURE http://SECRET_PROXY"))
			}))
			defer server.Close()
			_, err := FetchKiloStatus(context.Background(), server.Client(), server.URL, "MANAGEMENT_FIXTURE")
			if err == nil || strings.Contains(err.Error(), "SECRET") || redirectCalls != 0 {
				t.Fatalf("错误未脱敏或跟随重定向: %v", err)
			}
			if (code == 401 || code == 403) && !errors.Is(err, ErrKiloManagementAuth) {
				t.Fatal("管理认证失败分类错误")
			}
		})
	}
}

func TestKiloRejectsInvalidAndUnverifiedCapabilities(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"token":"SECRET`, kiloCapabilitiesFixture + `{}`, strings.Replace(kiloCapabilitiesFixture, `"upstream_billing_verified":false`, `"upstream_billing_verified":true`, 1), strings.Replace(kiloCapabilitiesFixture, `"auto_model_probe_default":false`, `"auto_model_probe_default":true`, 1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := FetchKiloCapabilities(context.Background(), server.Client(), server.URL, "MANAGEMENT_FIXTURE")
		server.Close()
		if !errors.Is(err, ErrKiloManagementResponse) || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("接受了无效能力回执: %v", err)
		}
	}
}

func TestKiloCapabilitiesAcceptOnlyNonemptyProtocolSubsets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		protocols []string
		valid     bool
	}{
		{"responses", []string{"responses"}, true},
		{"chat", []string{"chat_completions"}, true},
		{"both", []string{"responses", "chat_completions"}, true},
		{"both_reversed", []string{"chat_completions", "responses"}, true},
		{"empty", []string{}, false},
		{"null", nil, false},
		{"duplicate", []string{"responses", "responses"}, false},
		{"duplicate_chat", []string{"chat_completions", "chat_completions"}, false},
		{"unknown", []string{"responses", "unknown"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			protocols, err := json.Marshal(tc.protocols)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Replace(kiloCapabilitiesFixture, `["responses"]`, string(protocols), 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			result, err := FetchKiloCapabilities(context.Background(), server.Client(), server.URL, "MANAGEMENT_FIXTURE")
			if !tc.valid {
				if !errors.Is(err, ErrKiloManagementResponse) {
					t.Fatalf("接受了契约外协议: %+v %v", result, err)
				}
				return
			}
			if err != nil || len(result.PublicProtocols) != len(tc.protocols) {
				t.Fatalf("拒绝了合法协议子集: %+v %v", result, err)
			}
			for i, protocol := range tc.protocols {
				if result.PublicProtocols[i] != protocol {
					t.Fatalf("协议顺序或值变化: %+v", result.PublicProtocols)
				}
			}
		})
	}
}
