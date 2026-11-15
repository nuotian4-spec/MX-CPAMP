package chat_completions

import (
	"context"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

const textResponse = `{"id":"resp_literal","object":"response","model":"model","created_at":123,"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello world"}]}],"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}}}`

func TestResponsePreservesUpstreamFailure(t *testing.T) {
	for _, message := range []string{
		"basispoints returned an unsupported native tool; no tool was executed",
		"basispoints response violates the tool call count limit",
	} {
		r := ojson.NewDocString("{\"status\":\"failed\",\"output\":[]}")
		_ = r.Set("error.code", "basispoints_protocol_error")
		_ = r.Set("error.message", message)
		envelope := ojson.NewDocString("{\"type\":\"response.failed\"}")
		_ = envelope.SetRaw("response", string(r.Bytes()))
		for _, stream := range []bool{false, true} {
			for _, raw := range [][]byte{r.Bytes(), envelope.Bytes()} {
				if stream && !ojson.GetBytesNoCopy(raw, "type").Exists() {
					continue
				}
				var param any
				if stream {
					if frames := ConvertStream(context.Background(), "model", nil, nil, raw, &param); len(frames) != 0 {
						t.Fatal("failed event emitted success")
					}
				} else if body := ConvertResponse(context.Background(), "model", nil, nil, raw, &param); len(body) != 0 {
					t.Fatal("failed response emitted success")
				}
				err := param.(*responseState).err
				if err == nil {
					t.Fatal("missing upstream failure")
				}
				if ojson.Get(err.Error(), "error.code").String() != "basispoints_protocol_error" || ojson.Get(err.Error(), "error.message").String() != message {
					t.Fatalf("stream=%v error=%v", stream, err)
				}
			}
		}
	}
}

func TestUpstreamFailureFallback(t *testing.T) {
	for _, raw := range []string{"{}", "{\"error\":{}}", "{\"response\":{\"error\":{\"code\":\"known_code\"}}}"} {
		err := upstreamFailure(ojson.Parse(raw), "upstream response.failed")
		if !strings.Contains(err.Error(), "Responses to Chat: upstream response.failed") {
			t.Fatal(err)
		}
		if strings.Contains(raw, "known_code") && ojson.Get(err.Error(), "error.code").String() != "known_code" {
			t.Fatal(err)
		}
	}
}

func TestResponseBareAndEnvelope(t *testing.T) {
	for _, raw := range []string{textResponse, `{"type":"response.completed","response":` + textResponse + `}`} {
		var param any
		body := ConvertResponse(context.Background(), "fallback", nil, nil, []byte(raw), &param)
		if err := param.(*responseState).err; err != nil {
			t.Fatal(err)
		}
		for path, want := range map[string]string{"id": "resp_literal", "model": "model", "object": "chat.completion", "choices.0.message.content": "hello world", "choices.0.finish_reason": "stop"} {
			if ojson.GetBytesNoCopy(body, path).String() != want {
				t.Errorf("%s: %s", path, body)
			}
		}
		for path, want := range map[string]int64{"created": 123, "usage.prompt_tokens": 5, "usage.completion_tokens": 3, "usage.total_tokens": 8, "usage.prompt_tokens_details.cached_tokens": 2, "usage.completion_tokens_details.reasoning_tokens": 1} {
			if ojson.GetBytesNoCopy(body, path).Int() != want {
				t.Errorf("%s: %s", path, body)
			}
		}
	}
}

func TestResponseToolsRefusalAndIncomplete(t *testing.T) {
	for _, tc := range []struct{ name, raw, finish, field, want string }{
		{"function", `{"status":"completed","output":[{"type":"function_call","call_id":"call_raw","name":"ns.echo","arguments":" { \"q\": \"x\" } "}]}`, "tool_calls", "choices.0.message.tool_calls.0.function.arguments", ` { "q": "x" } `},
		{"refusal", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"no thank you"}]}]}`, "stop", "choices.0.message.refusal", "no thank you"},
		{"length", strings.Replace(textResponse, `"status":"completed"`, `"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}`, 1), "length", "choices.0.message.content", "hello world"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var param any
			body := ConvertResponse(context.Background(), "model", nil, fixtureCatalog, []byte(tc.raw), &param)
			if param.(*responseState).err != nil {
				t.Fatal(param.(*responseState).err)
			}
			if ojson.GetBytesNoCopy(body, "choices.0.finish_reason").String() != tc.finish || ojson.GetBytesNoCopy(body, tc.field).String() != tc.want {
				t.Fatal(string(body))
			}
		})
	}
}

func TestResponseRejectsFailureAndDuplicateCalls(t *testing.T) {
	for _, raw := range []string{`{"status":"failed","output":[]}`, `{"status":"in_progress","output":[]}`, `{"status":"completed","output":[{"type":"function_call","call_id":"same","name":"echo","arguments":"{}"},{"type":"function_call","call_id":"same","name":"echo","arguments":"{}"}]}`} {
		var param any
		body := ConvertResponse(context.Background(), "model", nil, fixtureCatalog, []byte(raw), &param)
		if body != nil || param.(*responseState).err == nil {
			t.Fatalf("invalid response returned success: %s", body)
		}
	}
}

func TestStreamTextUsageAndSingleTerminal(t *testing.T) {
	var param any
	var frames [][]byte
	for _, event := range []string{`{"type":"response.created","response":{"id":"resp_literal","model":"model","created_at":123}}`, `{"type":"response.output_text.delta","delta":"hello ","output_index":0,"content_index":0}`, `{"type":"response.output_text.delta","delta":"world","output_index":0,"content_index":0}`, `{"type":"response.output_text.done","text":"hello world","output_index":0,"content_index":0}`, `{"type":"response.completed","response":` + textResponse + `}`, `{"type":"response.completed","response":` + textResponse + `}`, `[DONE]`} {
		frames = append(frames, ConvertStream(context.Background(), "model", []byte(`{"stream_options":{"include_usage":true}}`), nil, []byte("event: response\ndata: "+event+"\n\n"), &param)...)
	}
	frames = append(frames, ConvertStream(context.Background(), "model", nil, nil, nil, &param)...)
	var text strings.Builder
	finish, usage := 0, 0
	for _, frame := range frames {
		text.WriteString(ojson.GetBytesNoCopy(frame, "choices.0.delta.content").String())
		if ojson.GetBytesNoCopy(frame, "choices.0.finish_reason").String() == "stop" {
			finish++
		}
		if ojson.GetBytesNoCopy(frame, "usage.total_tokens").Int() == 8 {
			usage++
			if len(ojson.GetBytesNoCopy(frame, "choices").Array()) != 0 {
				t.Fatal(string(frame))
			}
		}
	}
	if text.String() != "hello world" || finish != 1 || usage != 1 || param.(*responseState).err != nil {
		t.Fatalf("text=%q finish=%d usage=%d error=%v", text.String(), finish, usage, param.(*responseState).err)
	}
}

func TestStreamToolFragmentsPreserveIdentityAndArguments(t *testing.T) {
	var param any
	var frames [][]byte
	events := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"same_name","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{ \"x\":"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_2","type":"function_call","call_id":"call_2","name":"same_name","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_2","delta":"{}"}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":" 1 }"}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":"{ \"x\": 1 }"}`,
		`{"type":"response.completed","response":{"status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"same_name","arguments":"{ \"x\": 1 }"},{"id":"fc_2","type":"function_call","call_id":"call_2","name":"same_name","arguments":"{}"}]}}`,
	}
	for _, event := range events {
		frames = append(frames, ConvertStream(context.Background(), "model", nil, fixtureCatalog, []byte(event), &param)...)
	}
	args := map[int]string{}
	ids := map[int]string{}
	names := map[int]string{}
	terminals := 0
	for _, frame := range frames {
		for _, call := range ojson.GetBytesNoCopy(frame, "choices.0.delta.tool_calls").Array() {
			i := int(call.Get("index").Int())
			args[i] += call.Get("function.arguments").String()
			ids[i] += call.Get("id").String()
			names[i] += call.Get("function.name").String()
		}
		if ojson.GetBytesNoCopy(frame, "choices.0.finish_reason").String() == "tool_calls" {
			terminals++
		}
	}
	if args[0] != `{ "x": 1 }` || args[1] != "{}" || ids[0] != "call_1" || ids[1] != "call_2" || names[0] != "same_name" || names[1] != "same_name" || terminals != 1 || param.(*responseState).err != nil {
		t.Fatalf("args=%v ids=%v names=%v terminals=%d err=%v", args, ids, names, terminals, param.(*responseState).err)
	}
}

func TestStreamRefusalIncompleteAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, raw, finish string
		failed            bool
	}{
		{"refusal", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"no"}]}]}`, "stop", false},
		{"length", strings.Replace(textResponse, `"status":"completed"`, `"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}`, 1), "length", false},
		{"failed", `{"type":"response.failed","response":{"status":"failed"}}`, "", true},
		{"cancelled", `{"type":"response.cancelled"}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var param any
			raw := tc.raw
			if !tc.failed {
				raw = `{"type":"response.completed","response":` + raw + `}`
			}
			frames := ConvertStream(context.Background(), "model", nil, nil, []byte(raw), &param)
			s := param.(*responseState)
			if (s.err != nil) != tc.failed {
				t.Fatal(s.err)
			}
			if tc.failed && len(frames) != 0 {
				t.Fatal("failure emitted successful frames")
			}
			if !tc.failed && ojson.GetBytesNoCopy(frames[len(frames)-1], "choices.0.finish_reason").String() != tc.finish {
				t.Fatal(frames)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var param any
	if frames := ConvertStream(ctx, "model", nil, nil, []byte(textResponse), &param); len(frames) != 0 || param.(*responseState).err != context.Canceled {
		t.Fatal("cancelled stream manufactured success")
	}
}

var fixtureCatalog = []byte(`{"tools":[{"type":"function","name":"ns.echo"},{"type":"function","name":"same_name"},{"type":"function","name":"echo"},{"type":"function","name":"probe.echo"}]}`)

func TestResponseExplicitEmptyCompletion(t *testing.T) {
	for _, output := range []string{`[]`, `[{"type":"message","content":[{"type":"output_text","text":""}]}]`, `[{"type":"message","content":[{"type":"refusal","refusal":""}]}]`} {
		raw := []byte(`{"object":"response","status":"completed","output":` + output + `}`)
		var param any
		body := ConvertResponse(context.Background(), "model", nil, nil, raw, &param)
		if param.(*responseState).err != nil || ojson.GetBytesNoCopy(body, "choices.0.finish_reason").String() != "stop" {
			t.Fatalf("explicit completion rejected: %s", body)
		}
		param = nil
		frames := ConvertStream(context.Background(), "model", nil, nil, raw, &param)
		if param.(*responseState).err != nil || len(frames) == 0 || ojson.GetBytesNoCopy(frames[len(frames)-1], "choices.0.finish_reason").String() != "stop" {
			t.Fatal("explicit stream completion rejected")
		}
	}
}

func TestResponseNamespaceUsesOnlyEffectiveCatalog(t *testing.T) {
	item := `{"id":"item1","type":"function_call","namespace":"probe","name":"echo","call_id":"call1","arguments":" {} "}`
	raw := []byte(`{"status":"completed","output":[` + item + `]}`)
	var param any
	body := ConvertResponse(context.Background(), "model", nil, fixtureCatalog, raw, &param)
	if param.(*responseState).err != nil || ojson.GetBytesNoCopy(body, "choices.0.message.tool_calls.0.function.name").String() != "probe.echo" {
		t.Fatalf("qualified identity lost: %s %v", body, param.(*responseState).err)
	}
	param = nil
	body = ConvertResponse(context.Background(), "model", fixtureCatalog, []byte(`{"tools":[{"type":"function","name":"echo"}]}`), raw, &param)
	if body != nil || param.(*responseState).err == nil {
		t.Fatal("original catalog restored authorization")
	}
	param = nil
	var frames [][]byte
	for _, event := range []string{`{"type":"response.output_item.added","output_index":0,"item":` + item + `}`, `{"type":"response.output_item.done","output_index":0,"item":` + item + `}`, `{"type":"response.completed","response":` + string(raw) + `}`} {
		frames = append(frames, ConvertStream(context.Background(), "model", nil, fixtureCatalog, []byte(event), &param)...)
	}
	name, args := "", ""
	for _, frame := range frames {
		for _, call := range ojson.GetBytesNoCopy(frame, "choices.0.delta.tool_calls").Array() {
			name += call.Get("function.name").String()
			args += call.Get("function.arguments").String()
		}
	}
	if param.(*responseState).err != nil || name != "probe.echo" || args != " {} " {
		t.Fatalf("name=%q args=%q err=%v", name, args, param.(*responseState).err)
	}
}
