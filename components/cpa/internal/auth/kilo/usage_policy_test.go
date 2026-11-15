package kilo

import (
	"fmt"
	"testing"
	"time"
)

func TestAE1SourcePolicyExactScope(t *testing.T) {
	now := time.Unix(1900000000, 0).UTC()
	for _, tt := range []struct {
		name, used, secondary string
		scope                 string
		pause                 bool
	}{
		{"rounded", "99.999", "20", "18000+604800", false},
		{"exhausted", "100", "20", "18000+604800", true},
		{"weekly", "12", "100", "18000+604800", true},
		{"negative", "-1", "20", "", false},
		{"missing", "null", "20", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":%s,"reset_at":%d},"secondary_window":{"limit_window_seconds":604800,"used_percent":%s,"reset_at":%d}},"additional_rate_limits":[{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":100,"reset_at":%d}}}]}`, tt.used, now.Add(time.Hour).Unix(), tt.secondary, now.Add(24*time.Hour).Unix(), now.Add(time.Hour).Unix())
			obs, err := ParseUsage([]byte(body), now)
			if err != nil {
				t.Fatal(err)
			}
			scope, pause, _ := SourcePolicyScope(obs, now, now)
			if scope != tt.scope || pause != tt.pause {
				t.Fatalf("scope=%q pause=%v", scope, pause)
			}
		})
	}
}

func TestAE1SourcePolicyCycleBoundary(t *testing.T) {
	now := time.Unix(1900000000, 0).UTC()
	body := fmt.Sprintf(`{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":1,"reset_at":%d}}}`, now.Add(7*24*time.Hour).Unix())
	obs, err := ParseUsage([]byte(body), now)
	if err != nil {
		t.Fatal(err)
	}
	if scope, _, _ := SourcePolicyScope(obs, now.Add(-time.Second), now); scope != "" {
		t.Fatal("query straddling cycle accepted")
	}
	if scope, _, _ := SourcePolicyScope(obs, now, now); scope != "604800" {
		t.Fatal("current single-week rejected")
	}
}
