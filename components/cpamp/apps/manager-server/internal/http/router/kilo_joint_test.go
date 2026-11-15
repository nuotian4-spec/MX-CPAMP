package router

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// TestKiloManagerJointRuntime由CPA原生fixture启动，只接受测试进程的loopback入口。
func TestKiloManagerJointRuntime(t *testing.T) {
	cpaURL := os.Getenv("KILO_JOINT_CPA_URL")
	controlURL := os.Getenv("KILO_JOINT_CONTROL_URL")
	if cpaURL == "" || controlURL == "" {
		t.Skip("仅由CPA联合fixture执行")
	}
	for _, address := range []string{cpaURL, controlURL} {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("联合入口必须为本地合成HTTP服务")
		}
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("联合测试拒绝非loopback连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: "synthetic-management-key"}}
	handler := newKiloFixtureRouter(t, resolver, client)

	for _, key := range []string{"", "synthetic-kilo-key", "synthetic-management-key"} {
		rec := kiloRouterRequest(handler, http.MethodGet, "/api/kilo/accounts", key)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Manager管理门禁状态=%d，预期401", rec.Code)
		}
	}
	code, _ := kiloJointGET(t, client, cpaURL+"/v8/management/kilo/accounts", "synthetic-kilo-key")
	if code != http.StatusUnauthorized {
		t.Fatalf("CPA客户端key访问管理状态=%d，预期401", code)
	}

	var reference string
	readPhase := func(phase string, wantActive, wantBusy, wantHealthy int) {
		t.Helper()
		var accounts model.KiloAccounts
		var status model.KiloStatus
		var capabilities model.KiloCapabilities
		for _, resource := range []string{"accounts", "status", "capabilities"} {
			rec := kiloRouterRequest(handler, http.MethodGet, "/api/kilo/"+resource, kiloRouterAdminKey)
			if rec.Code != http.StatusOK {
				t.Fatalf("阶段=%s Manager %s 状态=%d", phase, resource, rec.Code)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("管理响应缺少no-store")
			}
			directStatus, direct := kiloJointGET(t, client, cpaURL+"/v8/management/kilo/"+resource, "synthetic-management-key")
			if directStatus != http.StatusOK {
				t.Fatalf("阶段=%s CPA %s 状态=%d", phase, resource, directStatus)
			}
			managerObject := kiloJointPublicObject(t, resource, rec.Body.Bytes(), true)
			cpaObject := kiloJointPublicObject(t, resource, direct, false)
			currentRef, ok := managerObject["connection_ref"].(string)
			if !ok || len(currentRef) != 32 {
				t.Fatal("Manager未返回既有连接引用")
			}
			if reference == "" {
				reference = currentRef
			} else if currentRef != reference {
				t.Fatal("同连接跨接口或阶段引用变化")
			}
			delete(managerObject, "connection_ref")
			if !reflect.DeepEqual(managerObject, cpaObject) {
				t.Fatalf("阶段=%s Manager与真实CPA的%s状态不一致", phase, resource)
			}
			var target any
			switch resource {
			case "accounts":
				target = &accounts
			case "status":
				target = &status
			case "capabilities":
				target = &capabilities
			}
			if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
		active := 0
		if len(accounts.Accounts) != 2 {
			t.Fatalf("原生合成账号数量=%d，预期2", len(accounts.Accounts))
		}
		for _, account := range accounts.Accounts {
			active += account.Active
			if !account.Enabled || account.EffectiveLimit != 20 || account.Waiting != 0 || account.QuotaSource != "none" || account.AllowDirectWhenProxyFull {
				t.Fatal("原生账号默认状态偏离契约")
			}
		}
		if active != wantActive || !status.Configured || status.AccountCount != 2 || status.BusyProxyCount != wantBusy || status.HealthyProxyCount != wantHealthy || status.AutoAccountProbeEnabled || status.AutoModelProbeEnabled {
			t.Fatalf("阶段=%s active=%d busy=%d healthy=%d，预期%d/%d/%d", phase, active, status.BusyProxyCount, status.HealthyProxyCount, wantActive, wantBusy, wantHealthy)
		}
		if capabilities.Channel != "kilo" || !reflect.DeepEqual(capabilities.PublicProtocols, []string{"responses", "chat_completions"}) || capabilities.AutoAccountProbeDefault || capabilities.AutoModelProbeDefault || capabilities.UpstreamBillingVerified {
			t.Fatal("真实CPA能力声明偏离冻结契约")
		}
		rec := kiloRouterRequest(handler, http.MethodGet, "/api/credential-concurrency", kiloRouterAdminKey)
		var snapshot concurrency.Snapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || snapshot.ConnectionRef != reference {
			t.Fatal("既有并发路由与Kilo未共用连接引用")
		}
		t.Logf("阶段=%s 三DTO与正式v8一致，账号=%d active=%d healthy=%d busy=%d，共享ref稳定且白名单无敏感值", phase, len(accounts.Accounts), active, status.HealthyProxyCount, status.BusyProxyCount)
	}

	readPhase("开始", 0, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cpaURL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"joint-synthetic","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	type inferenceResult struct {
		status int
		err    error
	}
	done := make(chan inferenceResult, 1)
	created := make(chan int, 1)
	go func() {
		res, err := (&http.Client{Transport: transport}).Do(req)
		if err != nil {
			done <- inferenceResult{err: err}
			return
		}
		reader := bufio.NewReader(res.Body)
		var frame strings.Builder
		for res.StatusCode == http.StatusOK {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				err = readErr
				break
			}
			if strings.TrimSpace(line) != "" {
				frame.WriteString(line)
				continue
			}
			found := false
			for _, part := range strings.Split(frame.String(), "\n") {
				if !strings.HasPrefix(part, "data:") {
					continue
				}
				var event struct {
					Type string `json:"type"`
				}
				if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(part, "data:"))), &event) == nil && event.Type == "response.created" {
					found = true
				}
			}
			frame.Reset()
			if found {
				created <- res.StatusCode
				_, err = io.Copy(io.Discard, reader)
				break
			}
		}
		_ = res.Body.Close()
		done <- inferenceResult{status: res.StatusCode, err: err}
	}()
	if status, _ := kiloJointGET(t, client, controlURL+"/started", ""); status != http.StatusOK {
		t.Fatalf("真实上游开始屏障状态=%d", status)
	}
	select {
	case status := <-created:
		if status != http.StatusOK {
			t.Fatalf("下游created屏障状态=%d", status)
		}
		t.Log("下游HTTP200及完整response.created帧已确认，随后取消")
	case result := <-done:
		t.Fatalf("下游created屏障前连接结束: status=%d err=%v", result.status, result.err)
	case <-time.After(5 * time.Second):
		t.Fatal("下游HTTP200/response.created屏障未到达")
	}
	readPhase("在途", 1, 1, 1)
	cancel()
	select {
	case result := <-done:
		if result.status != http.StatusOK {
			t.Fatalf("原生stream状态=%d，预期200；err=%v", result.status, result.err)
		}
		t.Logf("下游取消完成: status=%d err=%v", result.status, result.err)
	case <-time.After(5 * time.Second):
		t.Fatal("取消后推理连接未结束")
	}
	if status, _ := kiloJointGET(t, client, controlURL+"/released", ""); status != http.StatusOK {
		t.Fatalf("真实资源释放屏障状态=%d", status)
	}
	readPhase("取消后", 0, 0, 1)
	t.Log("真实Manager router -> 正式CPA v8 -> 同一Manager/KiloExecutor：在途active/busy=1，取消后归零；未替换管理DTO")
}

func kiloJointGET(t *testing.T, client *http.Client, address, key string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func kiloJointPublicObject(t *testing.T, resource string, raw []byte, manager bool) map[string]any {
	t.Helper()
	for _, value := range []string{"synthetic-kilo-token", "synthetic-kilo-key", "synthetic-management-key", kiloRouterAdminKey, "http://127.0.0.1", "local-proxy"} {
		if strings.Contains(string(raw), value) {
			t.Fatal("联合管理响应包含合成敏感值")
		}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	fields := map[string][]string{
		"accounts":     {"accounts"},
		"status":       {"configured", "account_count", "healthy_proxy_count", "busy_proxy_count", "auto_account_probe_enabled", "auto_model_probe_enabled"},
		"capabilities": {"channel", "public_protocols", "tools_semantics", "images_semantics", "auto_account_probe_default", "auto_model_probe_default", "upstream_billing_verified"},
	}[resource]
	if manager {
		fields = append(fields, "connection_ref")
	}
	kiloJointExactKeys(t, object, fields)
	if resource == "accounts" {
		accounts, ok := object["accounts"].([]any)
		if !ok {
			t.Fatal("账号DTO应为数组")
		}
		for _, rawAccount := range accounts {
			account, ok := rawAccount.(map[string]any)
			if !ok {
				t.Fatal("账号DTO应为对象")
			}
			kiloJointExactKeys(t, account, []string{"id", "name", "enabled", "priority", "organization_id", "allow_direct_when_proxy_full", "concurrency_limit", "effective_limit", "active", "waiting", "quota_source", "codex_credential_id"})
		}
		sort.Slice(accounts, func(i, j int) bool {
			return accounts[i].(map[string]any)["id"].(string) < accounts[j].(map[string]any)["id"].(string)
		})
	}
	return object
}

func kiloJointExactKeys(t *testing.T, object map[string]any, fields []string) {
	t.Helper()
	if len(object) != len(fields) {
		t.Fatalf("DTO字段数=%d，预期%d", len(object), len(fields))
	}
	for _, field := range fields {
		if _, exists := object[field]; !exists {
			t.Fatalf("DTO缺少白名单字段%s", field)
		}
	}
}
