package executor

import "github.com/MoeForge/moego/ojson"

// HasAccountBoundHistory identifies replay that is not portable between accounts.
func HasAccountBoundHistory(body []byte) bool {
	root := ojson.ParseBytes(body)
	if root.Get("previous_response_id").String() != "" {
		return true
	}
	var inspect func(ojson.Result) bool
	inspect = func(item ojson.Result) bool {
		if item.IsObject() {
			if item.Get("encrypted_content").String() != "" {
				return true
			}
			switch item.Get("type").String() {
			case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "compaction", "compaction_summary", "item_reference":
				return true
			}
			switch item.Get("role").String() {
			case "tool", "function":
				return true
			}
			if len(item.Get("tool_calls").Array()) != 0 || item.Get("function_call").Exists() {
				return true
			}
			for _, child := range item.Map() {
				if inspect(child) {
					return true
				}
			}
		} else if item.IsArray() {
			for _, child := range item.Array() {
				if inspect(child) {
					return true
				}
			}
		}
		return false
	}
	return inspect(root.Get("input")) || inspect(root.Get("messages"))
}

// BPSUnsupportedReason is shared by selection and the final read-only validation.
func BPSUnsupportedReason(body []byte) string {
	value, validObject := ojson.ParseBytes(body).Value().(map[string]any)
	if !ojson.ValidBytes(body) || !validObject {
		return "invalid JSON request"
	}
	if previous, _ := value["previous_response_id"].(string); previous != "" {
		return "previous_response_id requires the original provider"
	}
	if background, _ := value["background"].(bool); background {
		return "background responses are not supported"
	}
	if tools, ok := value["tools"].([]any); ok && len(tools) > 0 {
		return "client tools require the original provider"
	}
	for _, key := range []string{"temperature", "top_p", "max_output_tokens", "max_tokens", "max_completion_tokens"} {
		if v, exists := value[key]; exists && v != nil {
			return "BPS does not support " + key
		}
	}
	if v, _ := value["service_tier"].(string); v != "" && v != "auto" && v != "default" {
		return "BPS does not support service_tier"
	}
	var inspect func(any) bool
	inspect = func(v any) bool {
		switch item := v.(type) {
		case map[string]any:
			if encrypted, _ := item["encrypted_content"].(string); encrypted != "" {
				return true
			}
			if role, _ := item["role"].(string); role == "tool" || role == "function" {
				return true
			}
			if calls, ok := item["tool_calls"].([]any); ok && len(calls) > 0 {
				return true
			}
			if item["function_call"] != nil {
				return true
			}
			if kind, _ := item["type"].(string); kind != "" {
				switch kind {
				case "message", "text", "input_text", "output_text", "reasoning":
				default:
					return true
				}
			}
			for _, child := range item {
				if inspect(child) {
					return true
				}
			}
		case []any:
			for _, child := range item {
				if inspect(child) {
					return true
				}
			}
		}
		return false
	}
	for _, key := range []string{"input", "messages"} {
		if inspect(value[key]) {
			return "opaque, tool or non-text history requires the original provider and account"
		}
	}
	return ""
}
