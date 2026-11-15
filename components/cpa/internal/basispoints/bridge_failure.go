package basispoints

import (
	"encoding/json"
	"errors"
	"strings"
)

// BridgeFailure describes local conversion failure, never an upstream HTTP
// rejection. Only bounded structural metadata may be added to Details.
type BridgeFailure struct {
	Message                            string
	FailureStage                       string
	RejectedTool                       string
	RepairSkippedReason                string
	UpstreamTerminalEvent              string
	ToolBatchWithheld                  bool
	Reason, ToolType                   string
	ValidationReason                   string
	CatalogCount                       int
	SchemaTool, SchemaPath, SchemaRule string
}

func (f *BridgeFailure) Details() map[string]any {
	result := object{"type": "server_error", "code": "basispoints_protocol_error", "message": f.Message, "failure_stage": f.FailureStage}
	if f.RejectedTool != "" {
		result["rejected_tool"] = f.RejectedTool
	}
	if f.RepairSkippedReason != "" {
		result["repair_skipped_reason"] = f.RepairSkippedReason
	}
	if f.UpstreamTerminalEvent != "" {
		result["upstream_terminal_event"] = f.UpstreamTerminalEvent
	}
	result["tool_batch_withheld"] = f.ToolBatchWithheld
	result["reason"], result["tool_type"], result["catalog_count"] = f.Reason, f.ToolType, f.CatalogCount
	if f.ValidationReason != "" {
		result["validation_reason"] = BoundedToolValidationReason(f.ValidationReason)
	}
	if f.SchemaTool != "" {
		result["schema_tool"], result["schema_path"], result["schema_rule"] = f.SchemaTool, f.SchemaPath, f.SchemaRule
	}
	return result
}

// ParseBridgeFailure consumes the bridge's normalized output. Raw upstream
// failures pass through classifyUpstreamFailure first, which drops these fields.
func ParseBridgeFailure(raw []byte) *BridgeFailure {
	var payload object
	if decode(raw, &payload) != nil || text(payload["type"]) != "response.failed" {
		return nil
	}
	response, _ := payload["response"].(object)
	detail, _ := response["error"].(object)
	stage := text(detail["failure_stage"])
	if text(detail["code"]) != "basispoints_protocol_error" || (stage != "bridge_tool_validation" && stage != "bridge_protocol_conversion") {
		return nil
	}
	f := &BridgeFailure{Message: text(detail["message"]), FailureStage: stage,
		RejectedTool: safeSchemaName(text(detail["rejected_tool"])), RepairSkippedReason: text(detail["repair_skipped_reason"]),
		UpstreamTerminalEvent: text(detail["upstream_terminal_event"]), ToolBatchWithheld: detail["tool_batch_withheld"] == true}
	f.Reason = boundedToolReason(text(detail["reason"]))
	f.ToolType = boundedToolType(text(detail["tool_type"]))
	f.ValidationReason = BoundedToolValidationReason(text(detail["validation_reason"]))
	if rule := text(detail["schema_rule"]); rule != "" || f.ValidationReason != "" {
		f.SchemaTool = safeSchemaName(text(detail["schema_tool"]))
		f.SchemaPath = boundedArgumentPath(text(detail["schema_path"]))
		if rule != "" {
			f.SchemaRule = boundedArgumentRule(rule)
		}
	}
	if value, ok := detail["catalog_count"].(json.Number); ok {
		if n, err := value.Int64(); err == nil && n >= 0 && n <= 65536 {
			f.CatalogCount = int(n)
		}
	}
	return f
}

func boundedToolType(kind string) string {
	switch kind {
	case "function_call", "custom_tool_call", "native_tool", "mixed":
		return kind
	default:
		return "unknown"
	}
}

func boundedToolReason(reason string) string {
	switch reason {
	case "outside_catalog", "unsupported_native_tool", "missing_call_id", "duplicate_call_id", "call_count_limit", "schema_validation", "invalid_transport":
		return reason
	default:
		return "invalid_transport"
	}
}

func (b *Bridge) toolFailureMetadata(f *BridgeFailure, err error, response object) {
	f.CatalogCount = len(b.tools)
	f.Reason = ToolValidationReason(err)
	var schema toolArgumentsSchemaError
	if errors.As(err, &schema) {
		f.SchemaTool, f.SchemaPath, f.SchemaRule = schema.tool, schema.path, schema.rule
		f.ValidationReason = schema.reason
	}
	f.ToolType = "unknown"
	output, _ := response["output"].([]any)
	for _, raw := range output {
		item, _ := raw.(object)
		if !isTool(item) {
			continue
		}
		kind := boundedToolType(text(item["type"]))
		if kind == "unknown" {
			kind = "native_tool"
		}
		if f.ToolType != "unknown" && f.ToolType != kind {
			f.ToolType = "mixed"
			break
		}
		f.ToolType = kind
	}
}

// ToolValidationFailure carries the same bounded diagnostics for buffered server search rounds.
func (b *Bridge) ToolValidationFailure(response map[string]any, err error) *BridgeFailure {
	f := &BridgeFailure{FailureStage: "bridge_tool_validation", ToolBatchWithheld: true}
	if text(response["status"]) == "completed" {
		f.UpstreamTerminalEvent = "response.completed"
	}
	b.toolFailureMetadata(f, err, response)
	return f
}

func ToolValidationReason(err error) string {
	var unknown unknownClientToolError
	var schema toolArgumentsSchemaError
	switch {
	case errors.As(err, &unknown):
		return "outside_catalog"
	case errors.As(err, &schema):
		return "schema_validation"
	case strings.Contains(err.Error(), "unsupported native tool"):
		return "unsupported_native_tool"
	case strings.Contains(err.Error(), "tool call count limit"):
		return "call_count_limit"
	case strings.Contains(err.Error(), "missing call_id"):
		return "missing_call_id"
	case strings.Contains(err.Error(), "missing or duplicate tool call_id"), strings.Contains(err.Error(), "duplicate tool identities"):
		return "duplicate_call_id"
	}
	return "invalid_transport"
}
