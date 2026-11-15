package chat_completions

import (
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestRequestPreservesChatSemantics(t *testing.T) {
	raw := []byte(`{"model":"client-model","n":1,"stream":true,"stream_options":{"include_usage":true},"max_completion_tokens":123,"temperature":0.37,"top_p":0.83,"store":true,"parallel_tool_calls":false,"reasoning_effort":"low","response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}},"tools":[{"type":"function","function":{"name":"ns.echo","description":"literal tool","parameters":{"type":"object","properties":{"x":{"const":9007199254740993}}},"strict":false}}],"tool_choice":{"type":"function","function":{"name":"ns.echo"}},"messages":[{"role":"system","content":"system stays system"},{"role":"developer","content":"developer stays developer"},{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA==","detail":"high"}}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call_unchanged","type":"function","function":{"name":"ns.echo","arguments":"{ \"x\": 1 }"}}]},{"role":"tool","tool_call_id":"call_unchanged","content":"  result\nraw  "}]}`)
	body, err := ConvertRequest("routed-model", raw, true)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"model": "routed-model", "input.0.role": "system", "input.1.role": "developer", "input.2.content.1.detail": "high", "input.2.content.1.image_url": "data:image/png;base64,AA==", "input.3.call_id": "call_unchanged", "input.3.name": "ns.echo", "input.3.arguments": `{ "x": 1 }`, "input.4.call_id": "call_unchanged", "input.4.output": "  result\nraw  ", "tool_choice.name": "ns.echo", "text.format.type": "json_schema", "text.format.name": "answer", "reasoning.effort": "low"} {
		if got := ojson.GetBytesNoCopy(body, path).String(); got != want {
			t.Errorf("%s=%q want %q", path, got, want)
		}
	}
	for path, want := range map[string]string{"max_output_tokens": "123", "temperature": "0.37", "top_p": "0.83", "store": "true", "parallel_tool_calls": "false", "tools.0.strict": "false", "tools.0.parameters.properties.x.const": "9007199254740993"} {
		if got := ojson.GetBytesNoCopy(body, path).Raw; got != want {
			t.Errorf("%s=%s want %s", path, got, want)
		}
	}
	for _, path := range []string{"n", "stream_options", "messages", "include", "instructions"} {
		if ojson.GetBytesNoCopy(body, path).Exists() {
			t.Errorf("unexpected %s: %s", path, body)
		}
	}
}

func TestRequestExplicitlyRejectsUnmappedSemantics(t *testing.T) {
	for _, extra := range []string{`"n":2`, `"stop":["END"]`, `"logprobs":true`, `"seed":1`, `"audio":{}`, `"input":"Responses body"`, `"model messages":1`, `"max_tokens":8,"max_completion_tokens":9`, `"stream_options":{"unmapped":true}`, `"tools":[{"type":"custom","custom":{"name":"x"}}]`, `"tool_choice":{"type":"function","function":{"name":"undeclared"}}`} {
		t.Run(extra, func(t *testing.T) {
			body, err := ConvertRequest("model", []byte(`{"messages":[{"role":"user","content":"hi"}],`+extra+`}`), false)
			if err == nil || body != nil {
				t.Fatalf("unsupported input accepted: %s", body)
			}
		})
	}
}

func TestRequestKeepsNamesAndJSONObjects(t *testing.T) {
	name := strings.Repeat("long_name_", 12)
	id := strings.Repeat("call-id-", 12)
	d := ojson.NewDocString(`{"messages":[{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"arguments":"  {}  "}}]}],"response_format":{"type":"json_object"},"max_tokens":32}`)
	_ = d.Set("messages.0.tool_calls.0.id", id)
	_ = d.Set("messages.0.tool_calls.0.function.name", name)
	body, err := ConvertRequest("model", d.Bytes(), false)
	if err != nil {
		t.Fatal(err)
	}
	if ojson.GetBytesNoCopy(body, "input.0.name").String() != name || ojson.GetBytesNoCopy(body, "input.0.call_id").String() != id || ojson.GetBytesNoCopy(body, "input.0.arguments").String() != "  {}  " {
		t.Fatal(string(body))
	}
	if ojson.GetBytesNoCopy(body, "text.format.type").String() != "json_object" || ojson.GetBytesNoCopy(body, "max_output_tokens").Int() != 32 {
		t.Fatal(string(body))
	}
	for _, path := range []string{"store", "include", "reasoning", "parallel_tool_calls"} {
		if ojson.GetBytesNoCopy(body, path).Exists() {
			t.Errorf("Codex default leaked: %s", path)
		}
	}
}
