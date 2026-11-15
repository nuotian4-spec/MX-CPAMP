package chat_completions

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MoeForge/moego/ojson"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
)

type bridgeError struct {
	message string
	status  int
}

func (e *bridgeError) Error() string   { return e.message }
func (e *bridgeError) StatusCode() int { return e.status }
func invalid(path string) error {
	return &bridgeError{message: "Chat to Responses: unsupported or invalid " + path, status: 400}
}

func fields(value ojson.Result, path, allowed string) error {
	if !value.IsObject() {
		return invalid(path)
	}
	var err error
	value.ForEach(func(key, _ ojson.Result) bool {
		if !slices.Contains(strings.Fields(allowed), key.String()) {
			err = invalid(path + "." + key.String())
			return false
		}
		return true
	})
	return err
}

// ConvertRequest preserves standard Chat semantics without Codex-specific defaults.
func ConvertRequest(model string, raw []byte, stream bool) ([]byte, error) {
	if !ojson.ValidBytes(raw) {
		return nil, invalid("JSON")
	}
	r := ojson.ParseBytes(raw)
	if err := fields(r, "request", "model messages stream stream_options n tools tool_choice response_format max_tokens max_completion_tokens temperature top_p parallel_tool_calls store metadata user service_tier safety_identifier prompt_cache_key prompt_cache_retention reasoning_effort verbosity"); err != nil {
		return nil, err
	}
	if v := r.Get("n"); v.Exists() && (v.Type != ojson.Number || v.Float() != 1) {
		return nil, invalid("n (only 1 is supported)")
	}
	if v := r.Get("stream_options"); v.Exists() && v.Type != ojson.Null {
		if err := fields(v, "stream_options", "include_usage"); err != nil {
			return nil, err
		}
		if x := v.Get("include_usage"); x.Exists() && x.Type != ojson.True && x.Type != ojson.False {
			return nil, invalid("stream_options.include_usage")
		}
	}
	if v := r.Get("stream"); v.Exists() && v.Type != ojson.True && v.Type != ojson.False {
		return nil, invalid("stream")
	}
	if model == "" {
		model = r.Get("model").String()
	}
	out := ojson.NewDocString(`{"input":[]}`)
	_ = out.Set("model", model)
	_ = out.Set("stream", stream)
	for _, name := range strings.Fields("temperature top_p parallel_tool_calls store metadata user service_tier safety_identifier prompt_cache_key prompt_cache_retention") {
		if v := r.Get(name); v.Exists() {
			_ = out.SetRaw(name, v.Raw)
		}
	}
	a, b := r.Get("max_completion_tokens"), r.Get("max_tokens")
	if a.Exists() && b.Exists() && a.Raw != b.Raw {
		return nil, invalid("conflicting token limits")
	}
	if !a.Exists() {
		a = b
	}
	if a.Exists() {
		if a.Type != ojson.Number || a.Float() <= 0 || a.Float() != float64(a.Int()) {
			return nil, invalid("token limit")
		}
		_ = out.SetRaw("max_output_tokens", a.Raw)
	}
	if v := r.Get("reasoning_effort"); v.Exists() {
		_ = out.SetRaw("reasoning.effort", v.Raw)
	}
	if v := r.Get("verbosity"); v.Exists() {
		_ = out.SetRaw("text.verbosity", v.Raw)
	}
	if v := r.Get("response_format"); v.Exists() {
		if err := fields(v, "response_format", "type json_schema"); err != nil {
			return nil, err
		}
		switch v.Get("type").String() {
		case "text", "json_object":
			if v.Get("json_schema").Exists() {
				return nil, invalid("response_format.json_schema")
			}
			_ = out.Set("text.format.type", v.Get("type").String())
		case "json_schema":
			schema := v.Get("json_schema")
			if err := fields(schema, "response_format.json_schema", "name description schema strict"); err != nil {
				return nil, err
			}
			if schema.Get("name").String() == "" || !schema.Get("schema").IsObject() {
				return nil, invalid("response_format.json_schema")
			}
			_ = out.SetRaw("text.format", schema.Raw)
			_ = out.Set("text.format.type", "json_schema")
		default:
			return nil, invalid("response_format.type")
		}
	}
	names := map[string]bool{}
	if tools := r.Get("tools"); tools.Exists() {
		if !tools.IsArray() {
			return nil, invalid("tools")
		}
		values := [][]byte{}
		for i, tool := range tools.Array() {
			path := fmt.Sprintf("tools[%d]", i)
			if err := fields(tool, path, "type function"); err != nil {
				return nil, err
			}
			fn := tool.Get("function")
			if tool.Get("type").String() != "function" {
				return nil, invalid(path + ".type")
			}
			if err := fields(fn, path+".function", "name description parameters strict"); err != nil {
				return nil, err
			}
			name := fn.Get("name").String()
			if name == "" || names[name] {
				return nil, invalid(path + ".function.name")
			}
			names[name] = true
			d := ojson.NewDocString(fn.Raw)
			_ = d.Set("type", "function")
			values = append(values, d.Bytes())
		}
		translatorcommon.SetRawArrayItemsDoc(out, "tools", values)
	}
	if choice := r.Get("tool_choice"); choice.Exists() {
		if choice.Type == ojson.String {
			switch choice.String() {
			case "auto", "none", "required":
				_ = out.SetRaw("tool_choice", choice.Raw)
			default:
				return nil, invalid("tool_choice")
			}
		} else {
			if err := fields(choice, "tool_choice", "type function"); err != nil {
				return nil, err
			}
			if err := fields(choice.Get("function"), "tool_choice.function", "name"); err != nil {
				return nil, err
			}
			name := choice.Get("function.name").String()
			if choice.Get("type").String() != "function" || !names[name] {
				return nil, invalid("tool_choice.function.name")
			}
			_ = out.Set("tool_choice.type", "function")
			_ = out.Set("tool_choice.name", name)
		}
	}
	messages := r.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return nil, invalid("messages")
	}
	input := []any{}
	for i, message := range messages.Array() {
		path := fmt.Sprintf("messages[%d]", i)
		if err := fields(message, path, "role content tool_calls tool_call_id refusal"); err != nil {
			return nil, err
		}
		role := message.Get("role").String()
		if role == "tool" {
			if message.Get("tool_call_id").String() == "" || message.Get("content").Type != ojson.String || message.Get("tool_calls").Exists() || message.Get("refusal").Exists() {
				return nil, invalid(path)
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message.Get("tool_call_id").String(), "output": message.Get("content").String()})
			continue
		}
		if role != "user" && role != "assistant" && role != "system" && role != "developer" {
			return nil, invalid(path + ".role")
		}
		if message.Get("tool_call_id").Exists() {
			return nil, invalid(path + ".tool_call_id")
		}
		if role != "assistant" && (message.Get("tool_calls").Exists() || message.Get("refusal").Exists()) {
			return nil, invalid(path)
		}
		content, err := convertContent(message.Get("content"), role, path+".content")
		if err != nil {
			return nil, err
		}
		if refusal := message.Get("refusal"); refusal.Exists() && refusal.Type != ojson.Null {
			if refusal.Type != ojson.String {
				return nil, invalid(path + ".refusal")
			}
			content = append(content, map[string]any{"type": "refusal", "refusal": refusal.String()})
		}
		if len(content) > 0 {
			input = append(input, map[string]any{"type": "message", "role": role, "content": content})
		}
		if calls := message.Get("tool_calls"); calls.Exists() {
			if !calls.IsArray() {
				return nil, invalid(path + ".tool_calls")
			}
			for _, call := range calls.Array() {
				if err := fields(call, path+".tool_calls", "id type function"); err != nil {
					return nil, err
				}
				if err := fields(call.Get("function"), path+".tool_calls.function", "name arguments"); err != nil {
					return nil, err
				}
				if call.Get("type").String() != "function" || call.Get("id").String() == "" || call.Get("function.name").String() == "" || call.Get("function.arguments").Type != ojson.String {
					return nil, invalid(path + ".tool_calls")
				}
				input = append(input, map[string]any{"type": "function_call", "call_id": call.Get("id").String(), "name": call.Get("function.name").String(), "arguments": call.Get("function.arguments").String()})
			}
		}
		if len(content) == 0 && len(message.Get("tool_calls").Array()) == 0 {
			return nil, invalid(path + ".content")
		}
	}
	_ = out.Set("input", input)
	return out.Bytes(), nil
}

func convertContent(value ojson.Result, role, path string) ([]any, error) {
	content := []any{}
	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}
	if value.Type == ojson.String {
		return []any{map[string]any{"type": textType, "text": value.String()}}, nil
	}
	if !value.Exists() || value.Type == ojson.Null {
		return content, nil
	}
	if !value.IsArray() {
		return nil, invalid(path)
	}
	for _, part := range value.Array() {
		switch part.Get("type").String() {
		case "text":
			if err := fields(part, path, "type text"); err != nil {
				return nil, err
			}
			if part.Get("text").Type != ojson.String {
				return nil, invalid(path + ".text")
			}
			content = append(content, map[string]any{"type": textType, "text": part.Get("text").String()})
		case "image_url":
			if role != "user" {
				return nil, invalid(path + ".image_url")
			}
			if err := fields(part, path, "type image_url"); err != nil {
				return nil, err
			}
			img := part.Get("image_url")
			if err := fields(img, path+".image_url", "url detail"); err != nil {
				return nil, err
			}
			if img.Get("url").String() == "" {
				return nil, invalid(path + ".image_url.url")
			}
			item := map[string]any{"type": "input_image", "image_url": img.Get("url").String()}
			if detail := img.Get("detail"); detail.Exists() {
				if detail.String() != "auto" && detail.String() != "low" && detail.String() != "high" {
					return nil, invalid(path + ".image_url.detail")
				}
				item["detail"] = detail.String()
			}
			content = append(content, item)
		case "refusal":
			if role != "assistant" {
				return nil, invalid(path + ".refusal")
			}
			if err := fields(part, path, "type refusal"); err != nil {
				return nil, err
			}
			if part.Get("refusal").Type != ojson.String {
				return nil, invalid(path + ".refusal")
			}
			content = append(content, map[string]any{"type": "refusal", "refusal": part.Get("refusal").String()})
		default:
			return nil, invalid(path + ".type")
		}
	}
	return content, nil
}
