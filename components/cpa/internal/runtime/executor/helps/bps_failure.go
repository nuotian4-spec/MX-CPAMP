package helps

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func BPSToolValidationFailure(ctx context.Context, failure *basispoints.BridgeFailure, event []byte) error {
	diagnostic := NewBPSFailure("tool_validation", "tool_validation_failed")
	diagnostic.completed = failure.UpstreamTerminalEvent == "response.completed"
	diagnostic.withheld = failure.ToolBatchWithheld
	diagnostic.toolReason, diagnostic.toolType, diagnostic.catalogCount = failure.Reason, failure.ToolType, failure.CatalogCount
	diagnostic.schemaTool, diagnostic.schemaPath, diagnostic.schemaRule = failure.SchemaTool, failure.SchemaPath, failure.SchemaRule
	diagnostic.validationReason = basispoints.BoundedToolValidationReason(failure.ValidationReason)
	detail := BPSUsageDetail(ctx, event)
	diagnostic.usageDetail = &detail
	return diagnostic
}

// BPSFailure retains bounded diagnostics without the original error or response.
type BPSFailure struct {
	stage, code                        string
	upstreamHTTP, inbandStatus         int
	completed, withheld                bool
	usageDetail                        *usage.Detail
	toolReason, toolType               string
	validationReason                   string
	catalogCount                       int
	schemaTool, schemaPath, schemaRule string
}

func NewBPSFailure(stage, code string) *BPSFailure {
	switch stage + "/" + code {
	case "web_search/search_failed", "upstream_http/http_rejected", "upstream_inband/upstream_failed", "upstream_inband/unknown",
		"protocol_decode/invalid_response", "protocol_decode/incomplete_response",
		"protocol_conversion/conversion_failed", "tool_validation/tool_validation_failed",
		"binding_identity/identity_incomplete_or_changed", "binding_storage/storage_unavailable",
		"compaction_validation/invalid_compaction_state", "compaction_validation/conflicting_compaction_states", "compaction_validation/compaction_count_invalid",
		"response_translation/translation_empty":
		return &BPSFailure{stage: stage, code: code}
	default:
		return &BPSFailure{stage: "unknown", code: "unknown"}
	}
}

func (f *BPSFailure) BPSFailoverFacts() (bool, bool) {
	return f != nil && f.stage == "tool_validation" && f.completed, f != nil && f.withheld
}

// BPSFailoverReason exposes fixed local classifications without parsing message text.
func (f *BPSFailure) BPSFailoverReason() (string, string) {
	if f == nil {
		return "", ""
	}
	return f.toolReason, f.validationReason
}

// BPSFailureUsage retains bounded accounting only; arguments and response text are discarded.
func BPSFailureUsage(err error) (usage.Detail, bool) {
	var f *BPSFailure
	if errors.As(err, &f) && f.usageDetail != nil {
		return *f.usageDetail, true
	}
	return usage.Detail{}, false
}

func (f *BPSFailure) Error() string { return "BPS response failed" }

func BPSCorrectionInbandFailure(status, httpStatus int) error {
	f := NewBPSFailure("upstream_inband", "upstream_failed")
	f.inbandStatus = status
	return WithBPSHTTPStatus(f, httpStatus)
}

// BPSPermissionDenial exposes only trusted protocol status, never error text.
func (f *BPSFailure) BPSPermissionDenial() (string, int, bool) {
	if f != nil && f.stage == "upstream_inband" && f.inbandStatus == http.StatusForbidden {
		return "inband", f.upstreamHTTP, false
	}
	return "", 0, false
}

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
		errorType, code = "permission_error", "permission_denied"
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
	if failure.stage == "tool_validation" {
		if failure.validationReason != "" {
			_ = doc.Set("error.diagnostics.validation_reason", failure.validationReason)
		}
		if failure.schemaTool != "" {
			_ = doc.Set("error.diagnostics.tool", failure.schemaTool)
			_ = doc.Set("error.diagnostics.path", failure.schemaPath)
			_ = doc.Set("error.diagnostics.rule", failure.schemaRule)
		}
		_ = doc.Set("error.diagnostics.reason", failure.toolReason)
		_ = doc.Set("error.diagnostics.tool_type", failure.toolType)
		_ = doc.Set("error.diagnostics.catalog_count", failure.catalogCount)
		_ = doc.Set("error.diagnostics.upstream_completed", failure.completed)
		_ = doc.Set("error.diagnostics.tool_batch_withheld", failure.withheld)
		_ = doc.Set("error.diagnostics.usage_known", failure.usageDetail != nil && (failure.usageDetail.Iterations != nil && ((*failure.usageDetail.Iterations)[0].InputTokens != nil || (*failure.usageDetail.Iterations)[0].OutputTokens != nil)))
	}
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
