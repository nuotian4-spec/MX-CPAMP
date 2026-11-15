package chat_completions

import (
	"context"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

type responseState struct {
	err          error
	done         bool
	id           string
	model        string
	serviceTier  string
	created      int64
	started      bool
	includeUsage bool
	tools        map[int]*streamTool
	callIDs      map[string]int
	texts        map[string]string
	catalog      map[string]bool
}

func (s *responseState) ResponseTranslationError() error { return s.err }
func state(param *any) *responseState {
	if param != nil {
		if s, ok := (*param).(*responseState); ok {
			return s
		}
	}
	s := &responseState{tools: map[int]*streamTool{}, callIDs: map[string]int{}, texts: map[string]string{}}
	if param != nil {
		*param = s
	}
	return s
}
func (s *responseState) fail(message string) {
	s.err = &bridgeError{message: "Responses to Chat: " + message, status: 502}
	s.done = true
}

func upstreamFailure(r ojson.Result, fallback string) error {
	detail := r.Get("error")
	if !detail.IsObject() {
		detail = r.Get("response.error")
	}
	out := ojson.NewDocString("{}")
	if detail.IsObject() {
		_ = out.SetRaw("error", detail.Raw)
	} else if r.Get("type").String() == "error" {
		for _, field := range []string{"code", "message", "param"} {
			if v := r.Get(field); v.Exists() {
				_ = out.SetRaw("error."+field, v.Raw)
			}
		}
	}
	if !out.Get("error").Exists() {
		return &bridgeError{message: "Responses to Chat: " + fallback, status: 502}
	}
	if out.Get("error.message").String() == "" {
		_ = out.Set("error.message", "Responses to Chat: "+fallback)
	}
	return &bridgeError{message: string(out.Bytes()), status: 502}
}

func currentToolCatalog(request []byte) map[string]bool {
	names := map[string]bool{}
	for _, tool := range ojson.GetBytesNoCopy(request, "tools").Array() {
		if tool.Get("type").String() == "function" {
			names[tool.Get("name").String()] = true
		}
	}
	return names
}

func (s *responseState) toolName(item ojson.Result) string {
	name, namespace := item.Get("name").String(), item.Get("namespace").String()
	if namespace != "" && !strings.HasPrefix(name, namespace+".") {
		name = namespace + "." + name
	}
	if !s.catalog[name] {
		s.fail("function output is outside the current request tool catalog")
		return ""
	}
	return name
}

func responseObject(raw []byte) ojson.Result {
	r := ojson.ParseBytes(raw)
	if response := r.Get("response"); response.IsObject() {
		return response
	}
	return r
}
func finishReason(r ojson.Result) (string, error) {
	switch r.Get("status").String() {
	case "completed":
		return "stop", nil
	case "incomplete":
		switch r.Get("incomplete_details.reason").String() {
		case "max_output_tokens":
			return "length", nil
		case "content_filter":
			return "content_filter", nil
		}
	}
	return "", upstreamFailure(r, "upstream response is not complete ("+r.Get("status").String()+")")
}

func ConvertResponse(ctx context.Context, model string, _ []byte, request, raw []byte, param *any) []byte {
	s := state(param)
	s.catalog = currentToolCatalog(request)
	if ctx != nil && ctx.Err() != nil {
		s.err = ctx.Err()
		s.done = true
		return nil
	}
	if !ojson.ValidBytes(raw) {
		s.fail("invalid response JSON")
		return nil
	}
	r := responseObject(raw)
	finish, err := finishReason(r)
	if err != nil {
		s.err = err
		s.done = true
		return nil
	}
	if !r.Get("output").IsArray() {
		s.fail("missing response output")
		return nil
	}
	var text, refusal strings.Builder
	calls := []any{}
	for _, item := range r.Get("output").Array() {
		switch item.Get("type").String() {
		case "message":
			for _, part := range item.Get("content").Array() {
				switch part.Get("type").String() {
				case "output_text":
					text.WriteString(part.Get("text").String())
				case "refusal":
					refusal.WriteString(part.Get("refusal").String())
				default:
					s.fail("unsupported output content")
					return nil
				}
			}
		case "function_call":
			if item.Get("call_id").String() == "" || item.Get("name").String() == "" || item.Get("arguments").Type != ojson.String {
				s.fail("invalid function call")
				return nil
			}
			if _, exists := s.callIDs[item.Get("call_id").String()]; exists {
				s.fail("duplicate function call_id")
				return nil
			}
			s.callIDs[item.Get("call_id").String()] = len(calls)
			name := s.toolName(item)
			if s.err != nil {
				return nil
			}
			calls = append(calls, map[string]any{"id": item.Get("call_id").String(), "type": "function", "function": map[string]any{"name": name, "arguments": item.Get("arguments").String()}})
		case "reasoning":
		default:
			s.fail("unsupported output item " + item.Get("type").String())
			return nil
		}
	}
	if len(calls) > 0 && finish == "stop" {
		finish = "tool_calls"
	}
	out := ojson.NewDocString(`{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null},"finish_reason":"stop"}]}`)
	_ = out.Set("id", r.Get("id").String())
	_ = out.Set("created", r.Get("created_at").Int())
	if r.Get("model").String() != "" {
		model = r.Get("model").String()
	}
	_ = out.Set("model", model)
	if tier := r.Get("service_tier"); tier.Type == ojson.String && tier.String() != "" {
		_ = out.Set("service_tier", tier.String())
	}
	if text.Len() > 0 {
		_ = out.Set("choices.0.message.content", text.String())
	}
	if refusal.Len() > 0 {
		_ = out.Set("choices.0.message.refusal", refusal.String())
	}
	if len(calls) > 0 {
		_ = out.Set("choices.0.message.tool_calls", calls)
	}
	_ = out.Set("choices.0.finish_reason", finish)
	setUsage(out, r.Get("usage"))
	s.done = true
	return out.Bytes()
}
func setUsage(out *ojson.Doc, u ojson.Result) {
	if !u.IsObject() {
		return
	}
	_ = out.Set("usage.prompt_tokens", u.Get("input_tokens").Int())
	_ = out.Set("usage.completion_tokens", u.Get("output_tokens").Int())
	total := u.Get("total_tokens").Int()
	if !u.Get("total_tokens").Exists() {
		total = u.Get("input_tokens").Int() + u.Get("output_tokens").Int()
	}
	_ = out.Set("usage.total_tokens", total)
	if v := u.Get("input_tokens_details"); v.Exists() {
		_ = out.SetRaw("usage.prompt_tokens_details", v.Raw)
	}
	if v := u.Get("output_tokens_details"); v.Exists() {
		_ = out.SetRaw("usage.completion_tokens_details", v.Raw)
	}
}
