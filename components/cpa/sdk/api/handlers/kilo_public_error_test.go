package handlers

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloPublicFinalErrorBoundaries(t *testing.T) {
	ctx := logging.WithRequestID(channelscope.With(context.Background(), "kilo"), "synthetic-request-id")
	for _, tc := range []struct {
		name, code string
		err        error
		status     int
	}{
		{"local-size", "request_too_large", &helps.KiloPayloadError{Bytes: helps.KiloMaxRequestBytes + 1}, 413},
		{"local-busy", "request_preparation_busy", &helps.KiloPayloadError{Bytes: helps.KiloMaxRequestBytes + 1, Busy: true}, 503},
		{"connection", "upstream_connection_failed", &coreauth.Error{Code: coreauth.ErrorCodeConnectionLifecycle, HTTPStatus: 502, Message: "Kilo connection failed"}, 502},
		{"protocol", "upstream_response_invalid", helps.KiloProtocolFailure(errors.New("Kilo private-upstream https://kilo.ai/?token=synthetic-secret")), 502},
		{"unknown", "request_failed", errors.New("Kilo private-upstream https://kilo.ai/?token=synthetic-secret"), 500},
		{"terminal-auth", "upstream_authentication_required", coreauth.NewTerminalAuthError(&coreauth.Error{HTTPStatus: 401, Message: "Kilo private-upstream"}, errors.New("secret")), 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := executionErrorMessageForRequest(ctx, tc.err, false)
			if !errors.Is(msg.Error, tc.err) || msg.StatusCode != tc.status || msg.Addon != nil || msg.Headers != nil {
				t.Fatal("presentation lost original cause/status or retained upstream headers")
			}
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			NewBaseAPIHandlers(nil, nil).WriteErrorResponse(c, msg)
			bodies := []string{rec.Body.String(), string(BuildOpenAIResponsesStreamErrorChunk(tc.status, msg.Error.Error(), 2)), string(BuildOpenAIResponsesStreamFailedChunk(tc.status, msg.Error.Error(), 2))}
			for _, body := range bodies {
				for _, banned := range []string{"kilo", "private-upstream", "synthetic-secret"} {
					if strings.Contains(strings.ToLower(body), banned) {
						t.Fatalf("leak=%s", body)
					}
				}
				if !strings.Contains(body, tc.code) || !strings.Contains(body, "synthetic-request-id") {
					t.Fatalf("code/request ID missing: %s", body)
				}
			}
			if rec.Code != tc.status {
				t.Fatalf("status=%d", rec.Code)
			}
			if tc.name == "terminal-auth" && ojson.Get(rec.Body.String(), "error.retryable").Bool() {
				t.Fatal("terminal auth became retryable")
			}
		})
	}
	// The provider is never guessed from error prose; other channels are unchanged.
	for _, channel := range []string{"codex", "bps", ""} {
		err := presentationTestError{}
		want := executionErrorMessage(err)
		got := executionErrorMessageForRequest(channelscope.With(context.Background(), channel), err, false)
		if got.Error.Error() != want.Error.Error() || got.StatusCode != want.StatusCode || got.Addon.Get("X-Fixture") != "preserved" {
			t.Fatalf("changed %s error", channel)
		}
	}
	// Even a foreign/direct error on the dedicated channel cannot expose its body or headers.
	direct := executionErrorMessageForRequest(ctx, directPresentationTestError{}, false)
	if direct.DirectResponse || direct.Body != nil || direct.Headers != nil || direct.Addon != nil {
		t.Fatal("direct body bypassed channel presentation")
	}
}
