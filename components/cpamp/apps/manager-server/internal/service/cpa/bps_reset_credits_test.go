package cpa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBPSResetCreditsReadOnlyContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		present bool
		status  string
	}{
		{"missing", nil, false, "unknown"}, {"null", nil, true, "unknown"}, {"zero", 0, true, "known"}, {"positive", 3, true, "known"}, {"negative", -1, true, "unknown"}, {"fraction", 0.5, true, "unknown"}, {"unsafe", 9007199254740992.0, true, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gets, posts := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
				}
				if r.Method == "GET" {
					gets++
				}
				if r.URL.Path != "/v8/management/bps/credentials/quota" || r.URL.Query().Get("auth_id") != "id" || r.URL.Query().Get("auth_index") != "idx" || r.Header.Get("Authorization") != "Bearer test" {
					t.Error("identity or route changed")
				}
				body := map[string]any{"provider": "bps", "auth_id": "id", "auth_index": "idx", "source": "chatgpt_account_reset_credits", "queried_at": "2026-11-15T00:00:00Z"}
				if tc.present {
					body["available_count"] = tc.value
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			got, err := FetchBPSResetCredits(context.Background(), server.Client(), server.URL, "test", "idx", "id")
			if err != nil || got.Status != tc.status || gets != 1 || posts != 0 {
				t.Fatal(got, err, gets, posts)
			}
			if tc.status == "unknown" && got.Count != nil {
				t.Fatal("unknown became numeric")
			}
			if tc.name == "zero" && (got.Count == nil || *got.Count != 0) {
				t.Fatal("explicit zero lost")
			}
		})
	}
}
func TestBPSResetCreditsIdentityAndErrors(t *testing.T) {
	for _, change := range []string{"provider", "auth_id", "auth_index", "source", "queried_at", "query_error", "illegal_type"} {
		t.Run(change, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := map[string]any{"provider": "bps", "auth_id": "id", "auth_index": "idx", "source": "chatgpt_account_reset_credits", "queried_at": "2026-11-15T00:00:00Z", "available_count": 0}
				if change == "illegal_type" {
					body["available_count"] = "0"
				} else {
					body[change] = "SECRET_CANARY"
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			got, err := FetchBPSResetCredits(context.Background(), server.Client(), server.URL, "test", "idx", "id")
			if change == "query_error" {
				if err != nil || got.Status != "error" || got.Count != nil || got.Error == nil || *got.Error != "query_failed" {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("invalid identity accepted", got)
			}
		})
	}
}
