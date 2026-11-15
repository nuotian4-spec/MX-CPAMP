package bps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
)

func TestBPSOAuthHTTPStatusIsIndependentOfRetryStatus(t *testing.T) {
	transport := testTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"SECRET_DESCRIPTION"}`)), Header: make(http.Header)}, nil
	})
	_, err := NewClient("").Exchange(context.WithValue(t.Context(), "cliproxy.roundtripper", transport), "SECRET_CODE", "SECRET_VERIFIER")
	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) || oauthErr.Code != "invalid_grant" || oauthErr.StatusCode() != 401 || oauthErr.HTTPStatus() != 400 || oauthErr.ResponseReason() != "invalid_grant" {
		t.Fatalf("response status or existing retry classification changed: %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatal("error leaked upstream body")
	}
}

func TestBPSOAuthResponseFailuresPreserveStatusAndErrorText(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		status              int
		bodyError           bool
	}{
		{name: "read_body", message: "BPS OAuth response read failed", status: 200, bodyError: true},
		{name: "read_body_502", message: "BPS OAuth response read failed", status: 502, bodyError: true},
		{name: "malformed", body: "SECRET_BODY", message: "BPS OAuth response is invalid", status: 200},
		{name: "incomplete", body: `{"access_token":"SECRET_ACCESS"}`, message: "BPS OAuth response has incomplete tokens", status: 200},
	} {
		for _, refresh := range []bool{false, true} {
			mode := "exchange"
			if refresh {
				mode = "refresh"
			}
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				transport := testTransport(func(*http.Request) (*http.Response, error) {
					body := io.NopCloser(strings.NewReader(tc.body))
					if tc.bodyError {
						body = io.NopCloser(iotest.ErrReader(errors.New("SECRET_READ_ERROR")))
					}
					return &http.Response{StatusCode: tc.status, Body: body, Header: make(http.Header)}, nil
				})
				ctx := context.WithValue(t.Context(), "cliproxy.roundtripper", transport)
				client := NewClient("")
				var err error
				if refresh {
					_, err = client.Refresh(ctx, map[string]any{"client_id": ClientID, "refresh_token": "SECRET_REFRESH"})
				} else {
					_, err = client.Exchange(ctx, "SECRET_CODE", "SECRET_VERIFIER")
				}
				var responseErr interface{ HTTPStatus() int }
				if err == nil || err.Error() != tc.message || !errors.As(err, &responseErr) || responseErr.HTTPStatus() != tc.status {
					t.Fatalf("response status or original error text changed: %v", err)
				}
				var retryErr interface{ StatusCode() int }
				if errors.As(err, &retryErr) {
					t.Fatal("response failure acquired a retry status code")
				}
			})
		}
	}
}

func TestBPSOAuthResponseReasonIsIndependentOfRetryCode(t *testing.T) {
	for _, tc := range []struct{ upstream, reason string }{
		{upstream: "invalid_client", reason: "invalid_client"},
		{upstream: "SECRET_REASON", reason: "upstream_error"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			transport := testTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"error":"` + tc.upstream + `"}`)), Header: make(http.Header)}, nil
			})
			_, err := NewClient("").Exchange(context.WithValue(t.Context(), "cliproxy.roundtripper", transport), "SECRET_CODE", "SECRET_VERIFIER")
			var oauthErr *OAuthError
			if !errors.As(err, &oauthErr) || oauthErr.Code != "invalid_grant" || oauthErr.StatusCode() != 401 || oauthErr.HTTPStatus() != 401 || oauthErr.ResponseReason() != tc.reason {
				t.Fatalf("response reason or existing retry classification changed: %v", err)
			}
		})
	}
}
