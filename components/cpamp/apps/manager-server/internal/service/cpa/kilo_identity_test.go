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

func TestKiloClientIdentityI3Whitelist(t *testing.T) {
	const uuid = "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct{ name, input, want string }{
		{"旧字段缺失", "", ""},
		{"空对象值保持缺失", "null", ""},
		{"待初始化空档案", `{"state":"pending","profiles":[]}`, `{"state":"pending","profiles":[]}`},
		{"异常空档案", `{"state":"invalid","profiles":[],"error":"SECRET_ERROR"}`, `{"state":"invalid","profiles":[]}`},
		{"关联缺失和空数组区分", `{"state":"persisted","profiles":[{"ref":"profile-1"},{"ref":"profile-2","exits":[]}]}`, `{"state":"persisted","profiles":[{"ref":"profile-1"},{"ref":"profile-2","exits":[]}]}`},
		{"三类关联窄投影", `{"state":"persisted","token":"SECRET_TOKEN","machine_id":"` + uuid + `","profiles":[{"ref":"profile-1","id":"` + uuid + `","exits":[{"kind":"ip","ip":"8.8.8.8","proxy_url":"http://SECRET_PROXY"},{"kind":"direct","password":"SECRET_PASSWORD"},{"kind":"legacy","error":"SECRET_ERROR"}]}]}`, `{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"ip","ip":"8.8.8.8"},{"kind":"direct"},{"kind":"legacy"}]}]}`},
		{"规范IPv6", `{"state":"persisted","profiles":[{"ref":"profile-10","exits":[{"kind":"ip","ip":"2001:db8::1"}]}]}`, `{"state":"persisted","profiles":[{"ref":"profile-10","exits":[{"kind":"ip","ip":"2001:db8::1"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := kiloAccountFixture
			if tc.input != "" {
				body = strings.Replace(body, `"quota_source":`, `"client_identity":`+tc.input+`,"quota_source":`, 1)
			}
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/v8/management/kilo/accounts" || r.Header.Get("Authorization") != "Bearer I3_MANAGEMENT" {
					t.Error("身份读取越过现有只读接口")
				}
				_, _ = w.Write([]byte(body))
			}))
			defer upstream.Close()
			got, err := FetchKiloAccounts(context.Background(), upstream.Client(), upstream.URL, "I3_MANAGEMENT")
			if err != nil || len(got.Accounts) != 1 || calls != 1 {
				t.Fatalf("身份读取失败或产生额外请求: %v, 调用%d次", err, calls)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"SECRET", uuid, "machine_id", "proxy_url", "api_key", "token", "password"} {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("身份投影泄露 %s", secret)
				}
			}
			if tc.want == "" {
				if got.Accounts[0].ClientIdentity != nil || strings.Contains(string(raw), "client_identity") {
					t.Fatal("旧响应合成了身份状态")
				}
			} else {
				identity, err := json.Marshal(got.Accounts[0].ClientIdentity)
				if err != nil || string(identity) != tc.want {
					t.Fatalf("身份字段白名单或空数组语义变化: %s", identity)
				}
			}
		})
	}
}

func TestKiloClientIdentityI3RejectsMalformed(t *testing.T) {
	for _, input := range []string{
		`[]`, `"SECRET_VALUE"`, `{}`, `{"state":"unknown","profiles":[]}`,
		`{"state":"pending"}`, `{"state":"pending","profiles":null}`,
		`{"state":"persisted","profiles":[]}`,
		`{"state":"pending","profiles":[{"ref":"profile-1"}]}`,
		`{"state":"invalid","profiles":[{"ref":"profile-1"}]}`,
		`{"state":"persisted","profiles":{}}`,
		`{"state":"persisted","profiles":[null]}`,
		`{"state":"persisted","profiles":[{"ref":"11111111-1111-4111-8111-111111111111"}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-0"}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-01"}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1\nSECRET"}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1"},{"ref":"profile-1"}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":null}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":{}}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[null]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"unknown"}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"ip"}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"ip","ip":null}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"ip","ip":"SECRET_TOKEN"}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"ip","ip":"2001:0db8::1"}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"ip","ip":"fe80::1%eth0"}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"ip","ip":"::ffff:8.8.8.8"}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"direct","ip":"8.8.8.8"}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"direct","ip":null}]}]}`,
		`{"state":"persisted","profiles":[{"ref":"profile-1","exits":[{"kind":"legacy","ip":""}]}]}`,
	} {
		t.Run(input, func(t *testing.T) {
			body := strings.Replace(kiloAccountFixture, `"quota_source":`, `"client_identity":`+input+`,"quota_source":`, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer upstream.Close()
			_, err := FetchKiloAccounts(context.Background(), upstream.Client(), upstream.URL, "I3_MANAGEMENT")
			if !errors.Is(err, ErrKiloManagementResponse) || err.Error() != ErrKiloManagementResponse.Error() {
				t.Fatalf("非法身份对象未走既有脱敏协议错误: %v", err)
			}
		})
	}
}
