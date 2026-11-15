package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// TestKiloManagerClientIdentityI3JointRuntime 由 Core 父桥提供真实管理 Router。
func TestKiloManagerClientIdentityI3JointRuntime(t *testing.T) {
	cpaURL, controlURL := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL")
	if cpaURL == "" || controlURL == "" {
		t.Skip("仅由 Core I3 身份联合父桥执行")
	}
	loopback := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && u.Scheme == "http" && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
	}
	if !loopback(cpaURL) || !loopback(controlURL) {
		t.Fatal("联合入口必须为 loopback")
	}
	key := os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if key == "" {
		t.Fatal("缺少合成管理认证")
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("联合测试拒绝非 loopback 连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}
	h := newKiloFixtureRouter(t, resolver, client)
	uuid := regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	read := func() model.KiloAccounts {
		t.Helper()
		rec := kiloRouterRequest(h, http.MethodGet, "/api/kilo/accounts", kiloRouterAdminKey)
		assertKiloRouterSanitized(t, rec)
		for _, forbidden := range []string{"machine_id", "device_profiles", "password", key} {
			if strings.Contains(rec.Body.String(), forbidden) {
				t.Fatalf("身份回执透出 %s", forbidden)
			}
		}
		var result model.KiloAccounts
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.ConnectionRef == "" || uuid.Match(rec.Body.Bytes()) {
			t.Fatalf("真实身份读取或脱敏失败: %d", rec.Code)
		}
		return result
	}
	status, _ := kiloJointGET(t, client, controlURL+"/scenario", "")
	if status != http.StatusOK {
		t.Fatal("缺少 I3 场景")
	}
	first := read()
	if len(first.Accounts) != 6 {
		t.Fatalf("I3 场景应有六个合成账号，实际%d", len(first.Accounts))
	}
	find := func(id string) *model.KiloClientIdentity {
		t.Helper()
		for _, account := range first.Accounts {
			if account.ID == id && account.ClientIdentity != nil {
				return account.ClientIdentity
			}
		}
		t.Fatalf("缺少账号身份: %s", id)
		return nil
	}
	for _, state := range []string{"pending", "invalid"} {
		identity := find("i3-" + state + ".json")
		if identity.State != state || identity.Profiles == nil || len(identity.Profiles) != 0 {
			t.Fatalf("%s 状态或空数组语义错误", state)
		}
	}
	persisted := find("i3-persisted.json")
	if persisted.State != "persisted" || len(persisted.Profiles) != 3 {
		t.Fatal("已持久化三档案错误")
	}
	wantExits := []string{`[{"kind":"ip","ip":"8.8.8.8"}]`, `[{"kind":"direct"}]`, `[]`}
	for index, profile := range persisted.Profiles {
		raw, _ := json.Marshal(profile.Exits)
		if profile.Ref != fmt.Sprintf("profile-%d", index+1) || string(raw) != wantExits[index] {
			t.Fatalf("档案%d关联或闲置保留错误: %s", index+1, raw)
		}
	}
	legacy := find("i3-legacy.json")
	if legacy.State != "persisted" || len(legacy.Profiles) != 1 || legacy.Profiles[0].Ref != "profile-1" {
		t.Fatal("旧身份首档案投影错误")
	}
	legacyExits, _ := json.Marshal(legacy.Profiles[0].Exits)
	if string(legacyExits) != `[{"kind":"legacy"}]` {
		t.Fatal("旧出口被猜配为公网 IP")
	}
	stale := find("i3-stale.json")
	if stale.State != "persisted" || len(stale.Profiles) != 2 || stale.Profiles[0].Exits != nil || stale.Profiles[1].Exits != nil {
		t.Fatal("未知关系未保持缺失")
	}
	unbound := find("i3-unbound.json")
	if unbound.State != "persisted" || len(unbound.Profiles) != 1 || unbound.Profiles[0].Exits == nil || len(*unbound.Profiles[0].Exits) != 0 {
		t.Fatal("成功读取的无关联未保留显式空数组")
	}
	proxies := kiloProxySnapshot(t, h)
	if proxies.ConnectionRef != first.ConnectionRef || len(proxies.Proxies) != 2 {
		t.Fatal("身份读取改变同 IP 双入口或跨连接引用")
	}
	second := read()
	byID := func(accounts []model.KiloAccount) (map[string]model.KiloAccount, []string) {
		indexed := make(map[string]model.KiloAccount, len(accounts))
		order := make([]string, 0, len(accounts))
		for _, account := range accounts {
			indexed[account.ID] = account
			order = append(order, account.ID)
		}
		return indexed, order
	}
	firstAccounts, firstOrder := byID(first.Accounts)
	secondAccounts, secondOrder := byID(second.Accounts)
	fieldsEqual := reflect.DeepEqual(firstAccounts, secondAccounts)
	t.Logf("账号原顺序: 首读=%v; 次读=%v; 按账号ID对齐全字段相等=%v", firstOrder, secondOrder, fieldsEqual)
	if !fieldsEqual {
		for id, before := range firstAccounts {
			after, exists := secondAccounts[id]
			if !exists {
				t.Errorf("重复读取缺少账号: %s", id)
				continue
			}
			a, b := reflect.ValueOf(before), reflect.ValueOf(after)
			for index := 0; index < a.NumField(); index++ {
				if !reflect.DeepEqual(a.Field(index).Interface(), b.Field(index).Interface()) {
					t.Errorf("重复读取字段变化: %s.%s", id, a.Type().Field(index).Name)
				}
			}
		}
	}
	if first.ConnectionRef != second.ConnectionRef || !fieldsEqual || !reflect.DeepEqual(proxies, kiloProxySnapshot(t, h)) {
		t.Fatal("重复身份读取改变身份或出口容量快照")
	}
	resolver.replace(store.Setup{})
	unconfigured := kiloRouterRequest(h, http.MethodGet, "/api/kilo/accounts", kiloRouterAdminKey)
	if strings.Contains(unconfigured.Body.String(), "client_identity") || strings.Contains(unconfigured.Body.String(), "profile-1") {
		t.Fatal("切换未配置连接仍暴露旧身份")
	}
	response, err := client.Post(controlURL+"/scenario", "application/json", bytes.NewBufferString(`{"mode":"identity-i3"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatal("I3 最终场景回执失败")
	}
	t.Log("I3 六账号三态、三类出口、缺失与空数组、同 IP 双入口和连接隔离通过；重复 GET 身份/容量不变")
}
