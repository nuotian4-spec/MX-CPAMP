package helps

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/MoeForge/moego/ojson"
)

// BPSFailure retains bounded diagnostics without the original error or response.
type BPSFailure struct {
	stage, code                string
	upstreamHTTP, inbandStatus int
}

func NewBPSFailure(stage, code string) *BPSFailure {
	switch stage + "/" + code {
	case "upstream_http/http_rejected", "upstream_inband/upstream_failed", "upstream_inband/unknown",
		"protocol_decode/invalid_response", "protocol_decode/incomplete_response",
		"protocol_conversion/conversion_failed", "tool_validation/tool_validation_failed",
		"binding_identity/identity_incomplete_or_changed", "binding_storage/storage_unavailable",
		"response_translation/translation_empty":
		return &BPSFailure{stage: stage, code: code}
	default:
		return &BPSFailure{stage: "unknown", code: "unknown"}
	}
}

func (f *BPSFailure) Error() string { return "BPS response failed" }

func WithBPSHTTPStatus(err error, status int) error {
	if err == nil {
		return nil
	}
	failure := NewBPSFailure("unknown", "unknown")
	var original *BPSFailure
	if errors.As(err, &original) {
		*failure = *original
	}
	if status >= 100 && status <= 599 {
		failure.upstreamHTTP = status
	}
	return failure
}

// BPSFailureBody preserves the public HTTP classification and adds diagnostics.
func BPSFailureBody(err error, status int) string {
	failure := NewBPSFailure("unknown", "unknown")
	var original *BPSFailure
	if errors.As(err, &original) {
		failure = original
	}
	doc := ojson.NewDocString(`{"error":{"message":"BPS response failed","type":"invalid_request_error","diagnostics":{"stage":"unknown","code":"unknown","upstream_http_status":null,"inband_status":null}}}`)
	errorType, code := "invalid_request_error", ""
	switch status {
	case http.StatusUnauthorized:
		errorType, code = "authentication_error", "invalid_api_key"
	case http.StatusForbidden:
		errorType, code = "permission_error", "insufficient_quota"
	case http.StatusTooManyRequests:
		errorType, code = "rate_limit_error", "rate_limit_exceeded"
	case http.StatusNotFound:
		code = "model_not_found"
	case http.StatusRequestTimeout:
		errorType, code = "server_error", "request_timeout"
	default:
		if status >= http.StatusInternalServerError {
			errorType, code = "server_error", "internal_server_error"
		}
	}
	_ = doc.Set("error.type", errorType)
	if code != "" {
		_ = doc.Set("error.code", code)
	}
	_ = doc.Set("error.diagnostics.stage", failure.stage)
	_ = doc.Set("error.diagnostics.code", failure.code)
	if failure.upstreamHTTP >= 100 && failure.upstreamHTTP <= 599 {
		_ = doc.Set("error.diagnostics.upstream_http_status", failure.upstreamHTTP)
		if failure.stage == "upstream_http" {
			_ = doc.Set("error.message", fmt.Sprintf("BPS upstream returned HTTP %d", failure.upstreamHTTP))
		}
	}
	if failure.inbandStatus >= 400 && failure.inbandStatus <= 599 {
		_ = doc.Set("error.diagnostics.inband_status", failure.inbandStatus)
	}
	return doc.String()
}
