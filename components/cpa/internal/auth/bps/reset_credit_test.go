package bps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type resetCreditTransport func(*http.Request) (*http.Response, error)

func (f resetCreditTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBPSResetCreditsTopLevelCountAndErrors(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		want   int64
		code   string
	}{
		{`{"available_count":0}`, 200, 0, ""},
		{`{"available_count":2}`, 200, 2, ""},
		{`{"data":{"available_count":2}}`, 200, 0, "invalid_response"},
		{`{"available_count":-1}`, 200, 0, "invalid_response"},
		{`{"available_count":1.2}`, 200, 0, "invalid_response"},
		{`{"available_count":2} {}`, 200, 0, "invalid_response"},
		{`{"access_token":"CANARY"}`, 403, 0, "forbidden"},
		{`{}`, 302, 0, "upstream_error"},
	} {
		calls := 0
		tr := resetCreditTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != "GET" || r.URL.String() != ResetCreditsURL {
				t.Error(r.Method, r.URL.String())
			}
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{"Location": []string{"https://other.invalid"}}}, nil
		})
		ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", tr)
		count, err := NewClient("").ResetCreditsAvailable(ctx, "SYNTHETIC_TOKEN", "SYNTHETIC_ACCOUNT")
		if calls != 1 || count != tc.want || (tc.code == "" && err != nil) || (tc.code != "" && (err == nil || ResetCreditErrorCode(err) != tc.code)) {
			t.Fatalf("body=%s count=%d err=%v calls=%d", tc.body, count, err, calls)
		}
	}
}

func TestBPSResetConsumeDisconnectRemainsUnknown(t *testing.T) {
	calls := 0
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", resetCreditTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != ResetCreditsURL+"/consume" {
			t.Error(r.Method, r.URL.String())
		}
		return nil, errors.New("SYNTHETIC_SECRET_CANARY")
	}))
	err := NewClient("").ConsumeResetCredit(ctx, "TOKEN", "ACCOUNT", "11111111-1111-4111-8111-111111111111")
	var classified *ResetCreditError
	if calls != 1 || !errors.As(err, &classified) || !classified.Unknown || strings.Contains(err.Error(), "CANARY") {
		t.Fatal(calls, err)
	}
}

func TestBPSResetDoesNotInheritOAuthResponseTimeout(t *testing.T) {
	client := NewClient("")
	client.httpClient.Timeout = time.Nanosecond
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", resetCreditTransport(func(r *http.Request) (*http.Response, error) {
		if _, hasDeadline := r.Context().Deadline(); hasDeadline {
			t.Error("reset inherited a response deadline")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":2}`)), Header: http.Header{}}, nil
	}))
	if count, err := client.ResetCreditsAvailable(ctx, "TOKEN", "ACCOUNT"); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if client.httpClient.Timeout != time.Nanosecond {
		t.Fatal("OAuth client timeout changed")
	}
}
