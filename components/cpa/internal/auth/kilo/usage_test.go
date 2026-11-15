package kilo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloQuotaUsageWindowsCreditsAndReset(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	for _, test := range []struct {
		name, body string
		count      int
		credit     *float64
		field      string
		reset      int64
	}{
		{"short_week", `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":20,"reset_after_seconds":500},"secondary_window":{"limit_window_seconds":18000,"used_percent":50,"reset_after_seconds":100}},"spend_control":{"individual_limit":{"remaining":"7.5","limit":100,"used":99}},"credits":{"balance":8}}`, 2, usageTestNumber(7.5), "spend_control.individual_limit.remaining", 100},
		{"week_exhausted", `{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":100,"reset_after_seconds":100},"secondary_window":{"limit_window_seconds":604800,"used_percent":120,"reset_after_seconds":500}},"spend_control":{"individual_limit":{"limit":"10","used":"4"}}}`, 2, usageTestNumber(6), "spend_control.individual_limit.limit-used", 500},
		{"month_additional", `{"plan_type":"plus","rate_limit":{"primary_window":{"limit_window_seconds":2592000,"used_percent":40,"reset_at":"2027-01-15T08:00:00Z"}},"additional_rate_limits":[{"limit_name":"extra","rate_limit":{"secondary_window":{"limit_window_seconds":18000,"used_percent":25,"reset_after_seconds":80}}}],"credits":{"remaining":2,"balance":4}}`, 2, usageTestNumber(2), "credits.remaining", 0},
		{"week_milliseconds", `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":"10","reset_at":1800000600000}},"credits":{"balance":-1}}`, 1, usageTestNumber(0), "credits.balance", 600},
		{"unknown", `{"rate_limit":{"primary_window":{"limit_window_seconds":"NaN","used_percent":null,"reset_at":null}},"credits":{"remaining":"infinite","unit":"USD"}}`, 1, nil, "", 0},
		{"expired_reset", `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":100,"reset_at":1700000000,"reset_after_seconds":60}}}`, 1, nil, "", 0},
		{"unsupported_combo", `{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":100,"reset_after_seconds":60}}}`, 1, nil, "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := ParseUsage([]byte(test.body), now)
			if err != nil || len(out.Windows) != test.count {
				t.Fatalf("parse err=%v windows=%d", err, len(out.Windows))
			}
			if (out.Credits.Remaining == nil) != (test.credit == nil) || out.Credits.Unit != nil {
				t.Fatal("unknown credit/unit was fabricated")
			}
			if test.credit != nil && (*out.Credits.Remaining != *test.credit || out.Credits.SourceField == nil || *out.Credits.SourceField != test.field) {
				t.Fatal("credit priority differs")
			}
			reset := UsageRecoveryReset(out, now)
			if test.reset == 0 && reset != nil {
				t.Fatal("unsupported or past reset accepted")
			}
			if test.reset != 0 && (reset == nil || reset.Unix() != now.Unix()+test.reset) {
				t.Fatal("reset priority differs")
			}
			if test.name == "month_additional" && out.Windows[1].Group != "additional_rate_limits:0" {
				t.Fatal("additional group lost")
			}
		})
	}
	for _, body := range []string{`null`, `[]`, `{"token":"secret"}`, `broken`} {
		if _, err := ParseUsage([]byte(body), now); err == nil {
			t.Fatal("invalid usage accepted")
		}
	}
}
func usageTestNumber(n float64) *float64 { return &n }

type usageRoundTripper func(*http.Request) (*http.Response, error)

func (f usageRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestKiloQuotaUsageTransportBoundaries(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body, code string
	}{
		{"ok", 200, `{"credits":{}}`, ""}, {"unauthorized", 401, "private-upstream-secret", "usage_unauthorized"}, {"forbidden", 403, "private-upstream-secret", "usage_forbidden"}, {"rate", 429, "private-upstream-secret", "usage_rate_limited"}, {"redirect", 302, "private-upstream-secret", "usage_http_error"}, {"bad", 200, "broken", "usage_invalid_response"}, {"large", 200, strings.Repeat(" ", UsageMaxBytes+1), "usage_invalid_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: usageRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != UsageURL || r.Method != "GET" || r.Body != nil || r.Header.Get("Authorization") != "Bearer synthetic-usage" || r.Header.Get("ChatGPT-Account-Id") != "outer-account" {
					t.Fatal("usage protocol differs")
				}
				return &http.Response{StatusCode: test.status, Header: http.Header{"Location": []string{"https://other.invalid"}}, Body: io.NopCloser(strings.NewReader(test.body)), Request: r}, nil
			})}
			_, err := FetchUsage(context.Background(), client, "synthetic-usage", "outer-account")
			if calls != 1 {
				t.Fatal("usage retried or redirected")
			}
			if test.code == "" && err != nil {
				t.Fatal(err)
			}
			if test.code != "" && (err == nil || UsageErrorCode(err) != test.code) {
				t.Fatalf("code=%v", err)
			}
		})
	}
	t.Run("internal_budget", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		client := server.Client()
		base := client.Transport
		client.Transport = usageRoundTripper(func(r *http.Request) (*http.Response, error) {
			copy := r.Clone(r.Context())
			copy.URL.Scheme = "http"
			copy.URL.Host = strings.TrimPrefix(server.URL, "http://")
			return base.RoundTrip(copy)
		})
		_, err := fetchUsageBudget(context.Background(), client, "synthetic", "", 20*time.Millisecond)
		if UsageErrorCode(err) != "usage_network_error" {
			t.Fatal(err)
		}
	})
	t.Run("caller_cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := FetchUsage(ctx, &http.Client{}, "synthetic", "")
		if UsageErrorCode(err) != "usage_cancelled" {
			t.Fatal(err)
		}
	})
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("exp", time.Now().Add(-time.Hour).Unix())
}
