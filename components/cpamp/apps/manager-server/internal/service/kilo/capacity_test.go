package kilo

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCapacityWriteContract(t *testing.T) {
	cases := []struct {
		name, kind, body string
		accepted         bool
	}{
		{"按IP自动", "egress", `{"expected_revision":1,"mode":"auto","capacity_mode":"ip","allocation_mode":"auto","max_ips":5,"specified_ips":[],"candidate_proxy_ids":null,"exclusive_proxy_ids":[],"primary_proxy_id":""}`, true},
		{"指定集合", "egress", `{"expected_revision":1,"mode":"auto","capacity_mode":"ip","allocation_mode":"specified","max_ips":2,"specified_ips":["203.0.113.1","203.0.113.2"],"candidate_proxy_ids":null,"exclusive_proxy_ids":[],"primary_proxy_id":""}`, true},
		{"空指定集合", "egress", `{"expected_revision":1,"mode":"auto","capacity_mode":"ip","allocation_mode":"specified","max_ips":2,"specified_ips":[],"exclusive_proxy_ids":[],"primary_proxy_id":""}`, false},
		{"超额指定集合", "egress", `{"expected_revision":1,"mode":"auto","capacity_mode":"ip","allocation_mode":"specified","max_ips":1,"specified_ips":["203.0.113.1","203.0.113.2"],"exclusive_proxy_ids":[],"primary_proxy_id":""}`, false},
		{"非法IP", "egress", `{"expected_revision":1,"mode":"auto","capacity_mode":"ip","allocation_mode":"specified","max_ips":1,"specified_ips":["hostname"],"exclusive_proxy_ids":[],"primary_proxy_id":""}`, false},
		{"入口跟随", "proxy-pool", `{"expected_revision":1,"ip_concurrency_limit":20,"ip_entry_limit":null}`, true},
		{"入口低限额", "proxy-pool", `{"expected_revision":1,"ip_concurrency_limit":20,"ip_entry_limit":10}`, true},
		{"零容量", "proxy-pool", `{"expected_revision":1,"ip_concurrency_limit":0}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.body), &fields); err != nil {
				t.Fatal(err)
			}
			method, id := http.MethodPut, "account-1"
			if tc.kind == "proxy-pool" {
				method, id = http.MethodPatch, ""
			}
			_, err := validateProxyFields(method, tc.kind, id, fields)
			if (err == nil) != tc.accepted {
				t.Fatalf("接受状态不符: %v", err)
			}
		})
	}
}
