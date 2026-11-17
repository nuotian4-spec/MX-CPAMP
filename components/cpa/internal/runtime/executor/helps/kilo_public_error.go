package helps

import (
	"context"
	"errors"
	"net/http"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// KiloErrorPresentation is applied after scheduling. It never changes the cause
// or uses upstream prose to infer a restriction, quota, or authentication state.
type KiloErrorPresentation struct {
	Code, Type, Message        string
	InternalCode, InternalType string
	UpstreamStatus             int
	payload                    *KiloPayloadError
}

func (p KiloErrorPresentation) Body(requestID string) string {
	doc := ojson.NewDocString(`{"error":{}}`)
	_ = doc.Set("error.code", p.Code)
	_ = doc.Set("error.type", p.Type)
	_ = doc.Set("error.message", p.Message)
	if requestID != "" {
		_ = doc.Set("error.request_id", requestID)
	}
	if p.payload != nil {
		_ = doc.Set("error.actual_bytes", p.payload.Bytes)
		_ = doc.Set("error.limit_bytes", KiloMaxRequestBytes)
	}
	return doc.String()
}

// PresentKiloError must only receive errors known to belong to this executor or
// its dedicated client channel. Arbitrary machine identifiers remain internal.
func PresentKiloError(err error) KiloErrorPresentation {
	p := KiloErrorPresentation{Code: "request_failed", Type: "api_error", Message: "Request failed. Contact the administrator with the request ID."}
	set := func(code, kind, message string) KiloErrorPresentation {
		p.Code, p.Type, p.Message = code, kind, message
		return p
	}
	var response *KiloResponseError
	if errors.As(err, &response) {
		p.UpstreamStatus = response.upstreamStatus
		p.InternalCode, p.InternalType = kiloDiagnosticIdentifier(response.code), kiloDiagnosticIdentifier(response.kind)
	}
	if coreauth.IsTerminalAuthError(err) {
		return set("upstream_authentication_required", "authentication_error", "Service authorization is invalid. Contact the administrator.")
	}
	var payload *KiloPayloadError
	if errors.As(err, &payload) {
		p.payload, p.InternalCode = payload, payload.ErrorCode()
		if payload.Busy {
			return set("request_preparation_busy", "server_error", "Request preparation is busy. Try again later.")
		}
		return set("request_too_large", "invalid_request_error", "Request is too large. Reduce inline images or request size.")
	}
	var alpha *KiloAlphaError
	if errors.As(err, &alpha) {
		return set("invalid_request", "invalid_request_error", "Search request input was rejected. Check the search parameters.")
	}
	var alphaResult kiloAlphaResultError
	if errors.As(err, &alphaResult) {
		return set("upstream_response_invalid", "api_error", "Search response was invalid or incomplete.")
	}
	if response != nil {
		if response.code == "invalid_encrypted_content" {
			return set("invalid_encrypted_content", "invalid_request_error", "Encrypted input could not be accepted. Start a new conversation or resend valid input.")
		}
		if response.upstreamStatus == http.StatusRequestEntityTooLarge {
			return set("request_too_large", "invalid_request_error", "Request is too large. Reduce inline images or request size.")
		}
		for _, identifier := range []string{response.code, response.kind} {
			switch identifier {
			case "subscription_sharing_usage_limit_exceeded", "usage_limit_exceeded", "insufficient_quota":
				return set("upstream_quota_exhausted", "quota_error", "Service quota is exhausted. Contact the administrator.")
			case "invalid_api_key", "authentication_error":
				return set("upstream_authentication_required", "authentication_error", "Service authorization is invalid. Contact the administrator.")
			}
		}
		if response.upstreamStatus == http.StatusForbidden && (response.code == "account_restricted" || response.kind == "account_restricted") {
			return set("upstream_access_restricted", "permission_error", "Service access is restricted. Contact the administrator.")
		}
		switch response.upstreamStatus {
		case http.StatusUnauthorized:
			return set("upstream_authentication_required", "authentication_error", "Service authorization is invalid. Contact the administrator.")
		case http.StatusPaymentRequired:
			return set("upstream_payment_required", "permission_error", "Service billing requirements are not met. Contact the administrator.")
		case http.StatusForbidden:
			return set("upstream_access_denied", "permission_error", "Service access was denied. Contact the administrator.")
		}
		if response.upstreamStatus == http.StatusTooManyRequests || response.code == "rate_limit_exceeded" || response.code == "rate_limit_error" || response.kind == "rate_limit_error" || response.kind == "rate_limit_exceeded" {
			return set("rate_limit_exceeded", "rate_limit_error", "Request rate is limited. Try again later.")
		}
		if response.upstreamStatus == http.StatusBadRequest || response.upstreamStatus == http.StatusUnprocessableEntity {
			return set("invalid_request", "invalid_request_error", "Request input was rejected. Check the request format.")
		}
		return p
	}
	var protocol *kiloProtocolFailure
	if errors.As(err, &protocol) {
		return set("upstream_response_invalid", "api_error", "Service response was invalid or interrupted.")
	}
	var authError *coreauth.Error
	if errors.As(err, &authError) {
		p.InternalCode = kiloDiagnosticIdentifier(authError.Code)
		switch authError.Code {
		case coreauth.ErrorCodeConnectionLifecycle:
			return set("upstream_connection_failed", "api_error", "Service connection failed. Try again later.")
		case coreauth.ErrorCodeRequestScoped:
			if authError.HTTPStatus >= 400 && authError.HTTPStatus < 500 {
				return set("invalid_request", "invalid_request_error", "Request input was rejected. Check the request format.")
			}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return set("request_timeout", "server_error", "Request timed out.")
	}
	if errors.Is(err, context.Canceled) {
		return set("request_canceled", "invalid_request_error", "Request was canceled.")
	}
	return p
}

// Keep diagnostic identifiers bounded to recognized machine values. The cause
// still contains the original identifiers for scheduler decisions.
func kiloDiagnosticIdentifier(value string) string {
	switch value {
	case "", "subscription_sharing_usage_limit_exceeded", "usage_limit_exceeded", "insufficient_quota",
		"usage_limit_error", "rate_limit_exceeded", "rate_limit_error", "authentication_error", "invalid_api_key",
		"account_restricted", "invalid_encrypted_content", coreauth.ErrorCodeConnectionLifecycle, coreauth.ErrorCodeRequestScoped,
		"kilo_egress":
		return value
	default:
		return "unrecognized"
	}
}

type kiloProtocolFailure struct{ cause error }

func (e *kiloProtocolFailure) Error() string           { return e.cause.Error() }
func (e *kiloProtocolFailure) Unwrap() error           { return e.cause }
func (*kiloProtocolFailure) StatusCode() int           { return http.StatusBadGateway }
func (e *kiloProtocolFailure) PublicErrorBody() string { return PresentKiloError(e).Body("") }

// KiloProtocolFailure marks a verified response decoding/translation boundary;
// it does not infer an executor from error text or change its internal message.
func KiloProtocolFailure(cause error) error { return &kiloProtocolFailure{cause: cause} }
