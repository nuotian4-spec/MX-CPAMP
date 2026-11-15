package cpa

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCPABPSQuotaNormalizesSDKAndBindsCredential(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer TEST" {
			t.Error("请求边界错误")
		}
		switch r.URL.Path {
		case "/v8/management/credentials":
			fmt.Fprint(w, `{"files":[{"id":"one.json","name":"one.json","auth_index":"one","provider":"cpa-bps"}]}`)
		case "/v8/management/plugins/cpa-bps/quota":
			if r.URL.Query().Get("auth_index") != "one" {
				t.Error("凭据定位错误")
			}
			fmt.Fprint(w, `{"subscription":{"plan":"team"},"summary":[{"key":"cards","label":"卡片","value":0}],"groups":[{"displayName":"主额度","buckets":[{"window":"300m","remainingFraction":0.75,"resetTime":"2100-01-01T00:00:00Z"},{"window":"unknown"}]}]}`)
		default:
			t.Error("意外接口", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	got, err := FetchCPABPSQuota(context.Background(), upstream.Client(), upstream.URL, "TEST", "one", "one.json")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || got.Provider != "cpa-bps" || got.Source != "cpa_bps_plugin_quota" || got.AuthID != "one.json" || got.Quota.Subscription.Plan != "team" {
		t.Fatalf("%+v calls=%d", got, calls)
	}
	if len(got.Quota.Summary) != 1 || got.Quota.Summary[0].Value == nil || *got.Quota.Summary[0].Value != 0 {
		t.Fatal("summary合法零丢失", got)
	}
	buckets := got.Quota.Groups[0].Buckets
	if buckets[0].RemainingFraction == nil || *buckets[0].RemainingFraction != 0.75 || buckets[1].RemainingFraction != nil {
		t.Fatal(buckets)
	}
	windows := got.QuotaWindows()
	if len(windows) != 2 || windows[0].UsedPercent == nil || *windows[0].UsedPercent != 25 || windows[1].UsedPercent != nil {
		t.Fatal(windows)
	}
}

func TestCPABPSQuotaUnknownAndRejectedObservations(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		rejected     bool
		wantFraction bool
	}{
		{"空", `{}`, false, false},
		{"缺数值", `{"groups":[{"buckets":[{"window":"primary"}]}]}`, false, false},
		{"合法零", `{"groups":[{"buckets":[{"remainingFraction":0}]}]}`, false, true},
		{"越界", `{"groups":[{"buckets":[{"remainingFraction":2}]}]}`, false, false},
		{"过期", `{"groups":[{"buckets":[{"remainingFraction":0.8,"resetTime":"2000-01-01T00:00:00Z"}]}]}`, false, false},
		{"错日期", `{"groups":[{"buckets":[{"remainingFraction":0.8,"resetTime":"invalid"}]}]}`, false, false},
		{"错数值类型", `{"groups":[{"buckets":[{"remainingFraction":"0.5"}]}]}`, true, false},
		{"错provider", `{"provider":"bps"}`, true, false},
		{"错索引", `{"auth_index":"other"}`, true, false},
		{"错来源", `{"source":"chatgpt_account_usage"}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials" {
					fmt.Fprint(w, `{"files":[{"id":"one.json","auth_index":"one","provider":"cpa-bps"}]}`)
				} else {
					fmt.Fprint(w, tc.body)
				}
			}))
			defer server.Close()
			got, err := FetchCPABPSQuota(context.Background(), server.Client(), server.URL, "TEST", "one", "one.json")
			if (err != nil) != tc.rejected {
				t.Fatalf("err=%v", err)
			}
			if err != nil {
				return
			}
			known := false
			for _, g := range got.Quota.Groups {
				for _, b := range g.Buckets {
					known = known || b.RemainingFraction != nil
				}
			}
			if known != tc.wantFraction {
				t.Fatalf("已知额度=%v %+v", known, got)
			}
		})
	}
}

func TestCPABPSQuotaRejectsCredentialChangeAndWrongProvider(t *testing.T) {
	for _, change := range []bool{false, true} {
		reads := 0
		quotaCalls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v8/management/credentials" {
				reads++
				provider := "cpa-bps"
				if !change || reads > 1 {
					provider = "bps"
				}
				fmt.Fprintf(w, `{"files":[{"id":"one.json","auth_index":"one","provider":%q}]}`, provider)
			} else {
				quotaCalls++
				fmt.Fprint(w, `{}`)
			}
		}))
		_, err := FetchCPABPSQuota(context.Background(), server.Client(), server.URL, "TEST", "one", "one.json")
		server.Close()
		if err == nil || (!change && quotaCalls != 0) {
			t.Fatal("身份冲突仍读取", err, quotaCalls)
		}
	}
}
