package helps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloPublicErrorClassificationAndInternalFacts(t *testing.T) {
	for _, tc := range []struct {
		name, code, kind, public string
		upstream, status         int
	}{
		{"http-rate", "", "", "rate_limit_exceeded", 429, 429},
		{"http-payment", "", "", "upstream_payment_required", 402, 402},
		{"http-denied", "", "", "upstream_access_denied", 403, 403},
		{"http-auth", "", "", "upstream_authentication_required", 401, 401},
		{"http-restricted", "account_restricted", "", "upstream_access_restricted", 403, 403},
		{"stream-restricted-unknown", "account_restricted", "", "request_failed", 200, 502},
		{"stream-rate", "rate_limit_exceeded", "rate_limit_error", "rate_limit_exceeded", 200, 429},
		{"stream-quota", "subscription_sharing_usage_limit_exceeded", "usage_limit_error", "upstream_quota_exhausted", 200, 429},
		{"http-quota", "insufficient_quota", "", "upstream_quota_exhausted", 429, 429},
		{"stream-auth", "invalid_api_key", "authentication_error", "upstream_authentication_required", 200, 401},
		{"http-size", "too_large", "", "request_too_large", 413, 413},
		{"stream-encrypted", "invalid_encrypted_content", "", "invalid_encrypted_content", 200, 400},
		{"http-input", "invalid_request", "", "invalid_request", 400, 400},
		{"unknown-chinese", "request_failed", "api_error", "request_failed", 200, 502},
		{"unknown-private-identifiers", "https://kilo.ai/private?token=synthetic-secret", "Bearer synthetic-secret", "request_failed", 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := ojson.NewDocString(`{"error":{"message":"请求受限，请稍后重试。 private-upstream Bearer synthetic-secret https://kilo.ai/private"}}`)
			_ = doc.Set("error.code", tc.code)
			_ = doc.Set("error.type", tc.kind)
			err := kiloResponseError(tc.upstream, doc.Bytes())
			original := err.Error()
			public := PresentKiloError(channelscope.PreventReplay(err))
			body := public.Body("synthetic-request-id")
			if public.Code != tc.public || ojson.Get(body, "error.code").String() != tc.public || ojson.Get(body, "error.request_id").String() != "synthetic-request-id" {
				t.Fatalf("public classification=%s", body)
			}
			for _, forbidden := range []string{"kilo", "private-upstream", "synthetic-secret", "Bearer", "请求受限"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("public content leaked %q: %s", forbidden, body)
				}
			}
			if err.Error() != original || err.ErrorCode() != tc.code || err.kind != tc.kind || err.StatusCode() != tc.status || err.upstreamStatus != tc.upstream || err.IsRequestScoped() != (tc.upstream == 413 || tc.code == "invalid_encrypted_content") {
				t.Fatalf("internal facts changed: %+v", err)
			}
			if strings.Contains(public.InternalCode+public.InternalType, "synthetic-secret") {
				t.Fatal("unrecognized machine identifiers escaped diagnostic allowlist")
			}
		})
	}
}

func TestKiloPublicPayloadKeepsInternalContract(t *testing.T) {
	for _, busy := range []bool{false, true} {
		err := &KiloPayloadError{Bytes: KiloMaxRequestBytes + 1, Busy: busy}
		code, public, status := "kilo_request_too_large", "request_too_large", 413
		if busy {
			code, public, status = "kilo_image_preparation_busy", "request_preparation_busy", 503
		}
		want := fmt.Sprintf(`{"error":{"code":%q,"type":"invalid_request_error","message":"Kilo request exceeds the byte budget; reduce inline images or request size","actual_bytes":%d,"limit_bytes":%d}}`, code, err.Bytes, KiloMaxRequestBytes)
		if err.Error() != want || err.ErrorCode() != code || err.StatusCode() != status || !err.NoReplay() || !err.IsRequestScoped() {
			t.Fatalf("internal payload contract changed: %v", err)
		}
		if body := err.PublicErrorBody(); ojson.Get(body, "error.code").String() != public || strings.Contains(strings.ToLower(body), "kilo") {
			t.Fatalf("public payload=%s", body)
		}
	}
	inner := errors.New("Kilo invalid protocol https://kilo.ai/private Bearer synthetic-secret")
	wrapped := KiloProtocolFailure(inner)
	if !errors.Is(wrapped, inner) || wrapped.Error() != inner.Error() || PresentKiloError(wrapped).Code != "upstream_response_invalid" {
		t.Fatal("protocol marker changed its original cause")
	}
}

type kiloPublicRoundTripper func(*http.Request) (*http.Response, error)

func TestKiloPublicRequestScopePreservesServerFailure(t *testing.T) {
	for _, status := range []int{400, 409, 501, 503} {
		err := coreauth.NewRequestScopedError("Kilo private-upstream operation failed", status)
		want := "request_failed"
		if status < 500 {
			want = "invalid_request"
		}
		original := err.Error()
		if got := PresentKiloError(err).Code; got != want {
			t.Fatalf("status=%d public=%s want=%s", status, got, want)
		}
		if err.Error() != original || err.StatusCode() != status || !err.IsRequestScoped() {
			t.Fatal("request-scoped internal facts changed")
		}
	}
}

func (fn kiloPublicRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestKiloPublicTransportDropsPrivateCause(t *testing.T) {
	client := &http.Client{Transport: kiloPublicRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("https://kilo.ai/private Bearer synthetic-secret")
	})}
	a := &coreauth.Auth{Runtime: &kiloLease{client: client}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, KiloResponsesURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = KiloDo(context.Background(), a, req)
	var internal *coreauth.Error
	if !errors.As(err, &internal) || internal.Code != coreauth.ErrorCodeConnectionLifecycle || internal.HTTPStatus != 502 {
		t.Fatalf("transport internal error=%v", err)
	}
	if strings.Contains(err.Error(), "synthetic-secret") || PresentKiloError(err).Code != "upstream_connection_failed" {
		t.Fatalf("transport presentation=%s", PresentKiloError(err).Body(""))
	}
}
