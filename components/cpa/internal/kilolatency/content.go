package kilolatency

import (
	"bytes"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// Content accepts answer deltas, never reasoning, tool arguments, or terminal snapshots.
func Content(event []byte) (text, tool bool) {
	root := ojson.ParseBytes(event)
	switch root.Get("type").String() {
	case "response.output_text.delta":
		return strings.TrimSpace(root.Get("delta").String()) != "", false
	case "response.content_part.added":
		return root.Get("part.type").String() == "output_text" && strings.TrimSpace(root.Get("part.text").String()) != "", false
	case "response.output_item.added":
		kind := root.Get("item.type").String()
		if kind == "message" {
			for _, part := range root.Get("item.content").Array() {
				if part.Get("type").String() == "output_text" && strings.TrimSpace(part.Get("text").String()) != "" {
					return true, false
				}
			}
		}
		return false, strings.HasSuffix(kind, "_call")
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		return false, true
	case "response.completed":
		for _, item := range root.Get("response.output").Array() {
			if strings.HasSuffix(item.Get("type").String(), "_call") {
				return false, true
			}
		}
		return false, false
	}
	if root.Get("type").String() != "" {
		return false, false
	}
	for _, choice := range root.Get("choices").Array() {
		if strings.TrimSpace(choice.Get("delta.content").String()) != "" {
			text = true
		}
		if len(choice.Get("delta.tool_calls").Array()) > 0 {
			tool = true
		}
	}
	return text, tool
}

// Output tracks complete SSE frames actually written to the downstream writer.
// It stops retaining/parsing bytes as soon as the first answer is recognized.
type Output struct {
	pending []byte
	Text    bool
}

func (o *Output) Write(data []byte) {
	if o.Text {
		return
	}
	o.pending = append(o.pending, data...)
	for {
		end, width := bytes.Index(o.pending, []byte("\n\n")), 2
		if cr := bytes.Index(o.pending, []byte("\r\n\r\n")); cr >= 0 && (end < 0 || cr < end) {
			end, width = cr, 4
		}
		if end < 0 {
			return
		}
		var event []byte
		for _, line := range bytes.Split(o.pending[:end], []byte("\n")) {
			line = bytes.TrimSuffix(line, []byte("\r"))
			if bytes.HasPrefix(line, []byte("data:")) {
				event = append(event, bytes.TrimPrefix(line[5:], []byte(" "))...)
				event = append(event, '\n')
			}
		}
		o.pending = o.pending[end+width:]
		if text, _ := Content(event); text {
			o.Text = true
			o.pending = nil
			return
		}
		if len(o.pending) == 0 {
			o.pending = nil
			return
		}
	}
}
