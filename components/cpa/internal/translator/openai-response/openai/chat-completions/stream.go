package chat_completions

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

type streamTool struct {
	index     int
	itemID    string
	callID    string
	name      string
	arguments string
}

func (s *responseState) chunk(delta map[string]any, finish any) []byte {
	d := ojson.NewDocString(`{"object":"chat.completion.chunk","choices":[{"index":0}]}`)
	_ = d.Set("id", s.id)
	_ = d.Set("created", s.created)
	_ = d.Set("model", s.model)
	if s.serviceTier != "" {
		_ = d.Set("service_tier", s.serviceTier)
	}
	_ = d.Set("choices.0.delta", delta)
	_ = d.Set("choices.0.finish_reason", finish)
	return d.Bytes()
}
func (s *responseState) begin() [][]byte {
	if s.started {
		return nil
	}
	s.started = true
	return [][]byte{s.chunk(map[string]any{"role": "assistant", "content": ""}, nil)}
}
func (s *responseState) metadata(r ojson.Result, model string) {
	if tier := r.Get("service_tier"); tier.Type == ojson.String && tier.String() != "" {
		s.serviceTier = tier.String()
	}
	if id := r.Get("id").String(); id != "" {
		s.id = id
	}
	if m := r.Get("model").String(); m != "" {
		s.model = m
	} else if s.model == "" {
		s.model = model
	}
	if v := r.Get("created_at"); v.Exists() {
		s.created = v.Int()
	}
}
func eventJSON(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '{' {
		return raw
	}
	var data [][]byte
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if bytes.HasPrefix(line, []byte("data:")) {
			data = append(data, bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte{' '}))
		}
	}
	if len(data) > 0 {
		return bytes.Join(data, []byte{'\n'})
	}
	return raw
}

func ConvertStream(ctx context.Context, model string, original, request, raw []byte, param *any) [][]byte {
	s := state(param)
	if s.catalog == nil {
		s.catalog = currentToolCatalog(request)
	}
	if s.done {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		s.err = ctx.Err()
		s.done = true
		return nil
	}
	if len(raw) == 0 {
		s.fail("stream ended before a Responses terminal event")
		return nil
	}
	if !s.started {
		s.includeUsage = ojson.GetBytesNoCopy(original, "stream_options.include_usage").Bool()
	}
	body := eventJSON(raw)
	if bytes.Equal(body, []byte("[DONE]")) {
		s.fail("stream ended before a Responses terminal event")
		return nil
	}
	if !ojson.ValidBytes(body) {
		s.fail("invalid stream event")
		return nil
	}
	e := ojson.ParseBytes(body)
	kind := e.Get("type").String()
	switch kind {
	case "response.created", "response.in_progress", "response.queued":
		s.metadata(e.Get("response"), model)
		return nil
	case "response.failed", "response.cancelled", "response.canceled", "error":
		s.err = upstreamFailure(e, "upstream "+kind)
		s.done = true
		return nil
	case "response.completed", "response.incomplete":
		return s.terminal(e.Get("response"), model)
	case "response.output_item.added", "response.output_item.done":
		s.metadata(ojson.Result{}, model)
		item := e.Get("item")
		if item.Get("type").String() == "function_call" {
			return s.toolItem(int(e.Get("output_index").Int()), item, kind == "response.output_item.done")
		}
		if item.Get("type").String() != "message" && item.Get("type").String() != "reasoning" {
			s.fail("unsupported stream output item")
		}
		return nil
	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		s.metadata(ojson.Result{}, model)
		index := int(e.Get("output_index").Int())
		tool := s.tools[index]
		if tool == nil || e.Get("item_id").String() != tool.itemID {
			s.fail("unmatched function arguments event")
			return nil
		}
		if kind == "response.function_call_arguments.done" {
			return s.toolArguments(tool, e.Get("arguments").String(), true)
		}
		return s.toolArguments(tool, e.Get("delta").String(), false)
	case "response.output_text.delta", "response.output_text.done", "response.refusal.delta", "response.refusal.done":
		s.metadata(ojson.Result{}, model)
		field := "content"
		if strings.HasPrefix(kind, "response.refusal.") {
			field = "refusal"
		}
		key := fmt.Sprintf("%d/%d/%s", e.Get("output_index").Int(), e.Get("content_index").Int(), field)
		value := e.Get("delta").String()
		full := strings.HasSuffix(kind, ".done")
		if full {
			if field == "content" {
				value = e.Get("text").String()
			} else {
				value = e.Get("refusal").String()
			}
		}
		return s.text(key, field, value, full)
	case "response.content_part.added", "response.content_part.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.reasoning_text.delta", "response.reasoning_text.done":
		return nil
	default:
		if e.Get("object").String() == "response" {
			return s.terminal(e, model)
		}
		s.fail("unsupported stream event " + kind)
		return nil
	}
}

func (s *responseState) text(key, field, value string, full bool) [][]byte {
	previous := s.texts[key]
	if full {
		if !strings.HasPrefix(value, previous) {
			s.fail("inconsistent final text")
			return nil
		}
		value = strings.TrimPrefix(value, previous)
	}
	if value == "" {
		return nil
	}
	s.texts[key] = previous + value
	frames := s.begin()
	return append(frames, s.chunk(map[string]any{field: value}, nil))
}
func (s *responseState) toolItem(outputIndex int, item ojson.Result, full bool) [][]byte {
	name, callID, itemID := s.toolName(item), item.Get("call_id").String(), item.Get("id").String()
	if s.err != nil {
		return nil
	}
	if name == "" || callID == "" || item.Get("arguments").Type != ojson.String {
		s.fail("invalid streamed function call")
		return nil
	}
	tool := s.tools[outputIndex]
	var frames [][]byte
	if tool == nil {
		if _, exists := s.callIDs[callID]; exists {
			s.fail("duplicate function call_id")
			return nil
		}
		tool = &streamTool{index: len(s.tools), itemID: itemID, callID: callID, name: name}
		s.tools[outputIndex] = tool
		s.callIDs[callID] = outputIndex
		frames = s.begin()
		frames = append(frames, s.chunk(map[string]any{"tool_calls": []any{map[string]any{"index": tool.index, "id": callID, "type": "function", "function": map[string]any{"name": name, "arguments": ""}}}}, nil))
	} else if tool.name != name || tool.callID != callID || (tool.itemID != "" && itemID != "" && tool.itemID != itemID) {
		s.fail("inconsistent function identity")
		return nil
	}
	args := item.Get("arguments").String()
	if full || args != "" {
		frames = append(frames, s.toolArguments(tool, args, true)...)
	}
	return frames
}
func (s *responseState) toolArguments(tool *streamTool, value string, full bool) [][]byte {
	if full {
		if !strings.HasPrefix(value, tool.arguments) {
			s.fail("inconsistent final function arguments")
			return nil
		}
		value = strings.TrimPrefix(value, tool.arguments)
	}
	if value == "" {
		return nil
	}
	tool.arguments += value
	return [][]byte{s.chunk(map[string]any{"tool_calls": []any{map[string]any{"index": tool.index, "function": map[string]any{"arguments": value}}}}, nil)}
}
func (s *responseState) terminal(r ojson.Result, model string) [][]byte {
	s.metadata(r, model)
	finish, err := finishReason(r)
	if err != nil {
		s.err = err
		s.done = true
		return nil
	}
	if !r.Get("output").IsArray() {
		s.fail("missing terminal output")
		return nil
	}
	var frames [][]byte
	finalTools := 0
	finalText := map[string]bool{}
	for outputIndex, item := range r.Get("output").Array() {
		switch item.Get("type").String() {
		case "message":
			for contentIndex, part := range item.Get("content").Array() {
				field, value := "content", part.Get("text").String()
				switch part.Get("type").String() {
				case "output_text":
				case "refusal":
					field, value = "refusal", part.Get("refusal").String()
				default:
					s.fail("unsupported terminal content")
					return nil
				}
				key := fmt.Sprintf("%d/%d/%s", outputIndex, contentIndex, field)
				finalText[key] = true
				frames = append(frames, s.text(key, field, value, true)...)
			}
		case "function_call":
			finalTools++
			frames = append(frames, s.toolItem(outputIndex, item, true)...)
		case "reasoning":
		default:
			s.fail("unsupported terminal output item")
			return nil
		}
		if s.err != nil {
			return nil
		}
	}
	if finalTools != len(s.tools) {
		s.fail("terminal omitted a streamed function call")
		return nil
	}
	for key := range s.texts {
		if !finalText[key] {
			s.fail("terminal omitted streamed text")
			return nil
		}
	}
	if len(s.tools) > 0 && finish == "stop" {
		finish = "tool_calls"
	}
	frames = append(frames, s.begin()...)
	frames = append(frames, s.chunk(map[string]any{}, finish))
	if s.includeUsage && r.Get("usage").IsObject() {
		d := ojson.NewDoc(s.chunk(map[string]any{}, nil))
		_ = d.Set("choices", []any{})
		setUsage(d, r.Get("usage"))
		frames = append(frames, d.Bytes())
	}
	s.done = true
	return frames
}
