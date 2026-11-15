package cliproxy

import (
	"bytes"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

const kiloM7BImages = `[{"type":"text","text":"synthetic 图片"},{"type":"image_url","image_url":{"url":"data:image/png;base64,c3ludGhldGlj","detail":"high"}},{"type":"image_url","image_url":{"url":"https://synthetic.invalid/a.png","detail":"low"}}]`
const kiloM7BResponseImages = `[{"type":"input_text","text":"synthetic 图片"},{"type":"input_image","image_url":"data:image/png;base64,c3ludGhldGlj","detail":"high"},{"type":"input_image","image_url":"https://synthetic.invalid/a.png","detail":"low"}]`

func kiloM7BSeen(t *testing.T, r *http.Request, seen chan kiloSessionSeen) {
	t.Helper()
	session := r.Header.Get("X-Session-Id")
	if session == "" || session != r.Header.Get("X-KILOCODE-TASKID") || session != r.Header.Get("x-session-affinity") {
		t.Error("upstream session headers disagree")
	}
	seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), session}
}

func kiloM7BFixture(t *testing.T, kind, id string, additionalIDs ...string) (*kiloNativeFixture, chan kiloSessionSeen) {
	t.Helper()
	seen := make(chan kiloSessionSeen, 8)
	var count atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		kiloM7BSeen(t, r, seen)
		n := count.Add(1)
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("m7b-response-%d", n))
		if n == 1 && kind != "" {
			field := "arguments"
			if kind == "custom_tool_call" {
				field = "input"
			}
			items := []any{}
			for i, callID := range append([]string{id}, additionalIDs...) {
				items = append(items, map[string]any{"type": kind, "id": fmt.Sprintf("m7b-item-%d", i), "call_id": callID, "name": "source_tool", field: "参数 exact", "status": "completed"})
			}
			_ = doc.Set("response.output", items)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	return f, seen
}

func kiloM7BStart(t *testing.T, f *kiloNativeFixture, seen chan kiloSessionSeen, id string, chat ...bool) kiloSessionSeen {
	t.Helper()
	path, idPath := "/v1/responses", "output.0.call_id"
	if len(chat) > 0 && chat[0] {
		path, idPath = "/v1/chat/completions", "choices.0.message.tool_calls.0.id"
	}
	status, body := kiloSessionRequest(t, f, path, "synthetic-kilo-key", "m7b-initial", `{"model":"gpt-6.1-sol","input":"request source tool","messages":[{"role":"user","content":"request source tool"}]}`)
	if status != 200 || ojson.GetBytes(body, idPath).String() != id {
		t.Fatalf("initial call HTTP=%d body=%s", status, body)
	}
	first := <-seen
	t.Logf("registered call_id=%s length=%d account=%s session=%s public=%s", id, len(id), kiloAccountForMachine(t, f, first.Account), first.Session, body)
	<-f.requests
	f.assertReleased(t, 1)
	return first
}

func kiloM7BContinuation(kind, id string, history bool, content any) string {
	doc := ojson.NewDocString(`{"model":"gpt-6.1-sol"}`)
	messages := make([]any, 0, 2)
	if history {
		call := map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "source_tool", "arguments": "参数 exact"}}
		if kind == "custom_tool_call" {
			call = map[string]any{"id": id, "type": "custom", "custom": map[string]any{"name": "source_tool", "input": "参数 exact"}}
		}
		messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{call}})
	}
	messages = append(messages, map[string]any{"role": "tool", "tool_call_id": id, "content": content})
	_ = doc.Set("messages", messages)
	return doc.String()
}

func kiloM7BAssertRound(t *testing.T, f *kiloNativeFixture, seen chan kiloSessionSeen, first kiloSessionSeen, want int) {
	t.Helper()
	next := <-seen
	if next != first {
		t.Errorf("ownership moved: %+v -> %+v", first, next)
	}
	f.assertReleased(t, want)
	if f.posts.Load() != int32(want) {
		t.Errorf("unexpected POST count=%d want=%d", f.posts.Load(), want)
	}
	for _, result := range f.hook.snapshot() {
		if !result.Success {
			t.Errorf("unexpected failed result=%+v", result)
		}
	}
	t.Logf("POST=%d account=%s session=%s released", f.posts.Load(), kiloAccountForMachine(t, f, next.Account), next.Session)
}

func TestKiloM7BUserImageDetail(t *testing.T) {
	for _, protocol := range []string{"responses", "chat/completions"} {
		t.Run(protocol, func(t *testing.T) {
			f, seen := kiloM7BFixture(t, "", "")
			doc := ojson.NewDocString(`{"model":"gpt-6.1-sol"}`)
			if protocol == "responses" {
				_ = doc.Set("input", []any{map[string]any{"role": "user", "content": ojson.Parse(kiloM7BResponseImages).Value()}})
			} else {
				_ = doc.Set("messages", []any{map[string]any{"role": "user", "content": ojson.Parse(kiloM7BImages).Value()}})
			}
			status, body := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", doc.String())
			if status != 200 {
				t.Fatalf("HTTP=%d body=%s", status, body)
			}
			wire := <-f.requests
			t.Logf("source=%s wire=%s", doc.String(), wire)
			if !reflect.DeepEqual(ojson.GetBytes(wire, "input.0.content").Value(), ojson.Parse(kiloM7BResponseImages).Value()) {
				t.Errorf("image bytes/detail/order differ: %s", wire)
			}
			owner := <-seen
			seen <- owner
			kiloM7BAssertRound(t, f, seen, owner, 1)
		})
	}
}

func TestKiloM7BToolImageContinuations(t *testing.T) {
	for _, kind := range []string{"function_call", "custom_tool_call"} {
		for _, history := range []bool{true, false} {
			for _, representation := range []string{"array", "image-json-string", "ordinary-json-string", "ordinary-text"} {
				t.Run(fmt.Sprintf("%s/history=%t/%s", kind, history, representation), func(t *testing.T) {
					id := "call-m7b-image"
					f, seen := kiloM7BFixture(t, kind, id)
					first := kiloM7BStart(t, f, seen, id)
					var content any = ojson.Parse(kiloM7BImages).Value()
					var want any = ojson.Parse(kiloM7BResponseImages).Value()
					switch representation {
					case "image-json-string":
						content = kiloM7BImages
					case "ordinary-json-string":
						content = `  {"result":"原样", "n":1}  `
						want = content
					case "ordinary-text":
						content = "原样 非JSON\n result"
						want = content
					}
					source := kiloM7BContinuation(kind, id, history, content)
					status, body := f.request(t, "POST", "/v1/chat/completions", "synthetic-kilo-key", source)
					if status != 200 {
						t.Fatalf("continuation HTTP=%d body=%s", status, body)
					}
					wire := <-f.requests
					t.Logf("source=%s wire=%s", source, wire)
					outputs, calls := 0, 0
					for _, item := range ojson.GetBytes(wire, "input").Array() {
						if item.Get("type").String() == kind {
							calls++
						}
						if item.Get("type").String() == kind+"_output" {
							outputs++
							if item.Get("call_id").String() != id || !reflect.DeepEqual(item.Get("output").Value(), want) {
								t.Errorf("tool output differs: %s", item.Raw)
							}
						}
					}
					if outputs != 1 || (history && calls != 1) || (!history && calls != 0) {
						t.Errorf("unexpected history/output cardinality: calls=%d outputs=%d", calls, outputs)
					}
					kiloM7BAssertRound(t, f, seen, first, 2)
				})
			}
		}
	}
}

func TestKiloM7BLongCallID(t *testing.T) {
	for _, kind := range []string{"function_call", "custom_tool_call"} {
		for _, route := range []string{"responses", "chat-history", "chat-tool-only"} {
			t.Run(kind+"/"+route, func(t *testing.T) {
				id := strings.Repeat("k", 80)
				f, seen := kiloM7BFixture(t, kind, id)
				first := kiloM7BStart(t, f, seen, id, route != "responses")
				path := "/v1/chat/completions"
				source := kiloM7BContinuation(kind, id, route == "chat-history", "exact result")
				if route == "responses" {
					path = "/v1/responses"
					doc := ojson.NewDocString(`{"model":"gpt-6.1-sol"}`)
					_ = doc.Set("input", []any{map[string]any{"type": kind + "_output", "call_id": id, "output": "exact result"}})
					source = doc.String()
				}
				status, body := f.request(t, "POST", path, "synthetic-kilo-key", source)
				if status != 200 {
					f.assertReleased(t, 2)
					t.Fatalf("continuation HTTP=%d POST=%d body=%s", status, f.posts.Load(), body)
				}
				wire := <-f.requests
				t.Logf("source=%s wire=%s", source, wire)
				outputs, calls := 0, 0
				for _, item := range ojson.GetBytes(wire, "input").Array() {
					if item.Get("call_id").String() != id {
						t.Errorf("original long ID lost: %s", item.Raw)
					}
					if item.Get("type").String() == kind+"_output" {
						outputs++
					}
					if item.Get("type").String() == kind {
						calls++
					}
				}
				if outputs != 1 || (route == "chat-history" && calls != 1) || (route != "chat-history" && calls != 0) {
					t.Errorf("duplicate or lost call/output calls=%d outputs=%d", calls, outputs)
				}
				kiloM7BAssertRound(t, f, seen, first, 2)
			})
		}
	}
}

func TestKiloM7BRepeatedImageURLOrder(t *testing.T) {
	f, seen := kiloM7BFixture(t, "", "")
	status, body := kiloSessionRequest(t, f, "/v1/chat/completions", "synthetic-kilo-key", "repeated-images", `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"start"}]}`)
	if status != 200 {
		t.Fatalf("initial HTTP=%d body=%s", status, body)
	}
	first := <-seen
	<-f.requests
	f.assertReleased(t, 1)
	doc := ojson.NewDocString(`{"model":"gpt-6.1-sol"}`)
	messages := []any{}
	for _, detail := range []string{"high", "low", "auto"} {
		messages = append(messages, map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": detail},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://synthetic.invalid/same.png", "detail": detail}},
		}})
		messages = append(messages, map[string]any{"role": "assistant", "content": "ack " + detail})
	}
	_ = doc.Set("messages", messages)
	status, body = kiloSessionRequest(t, f, "/v1/chat/completions", "synthetic-kilo-key", "repeated-images", doc.String())
	if status != 200 {
		t.Fatalf("HTTP=%d body=%s", status, body)
	}
	wire := <-f.requests
	for i, detail := range []string{"high", "low", "auto"} {
		item := ojson.GetBytes(wire, fmt.Sprintf("input.%d", i*2))
		if item.Get("content.0.text").String() != detail || item.Get("content.1.detail").String() != detail || item.Get("content.1.image_url").String() != "https://synthetic.invalid/same.png" {
			t.Errorf("same URL detail moved across messages: %s", wire)
		}
	}
	t.Logf("source=%s wire=%s", doc.String(), wire)
	kiloM7BAssertRound(t, f, seen, first, 2)
}

func TestKiloM7BNormalToolImagePassthrough(t *testing.T) {
	for _, kind := range []string{"function_call", "custom_tool_call"} {
		t.Run(kind, func(t *testing.T) {
			id := "normal-image-call"
			f, seen := kiloM7BFixture(t, kind, id)
			first := kiloM7BStart(t, f, seen, id)
			doc := ojson.NewDocString(`{"model":"gpt-6.1-sol"}`)
			_ = doc.Set("input", []any{map[string]any{"type": kind + "_output", "call_id": id, "output": ojson.Parse(kiloM7BResponseImages).Value(), "synthetic_extension": "exact"}})
			status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", doc.String())
			if status != 200 {
				t.Fatalf("HTTP=%d body=%s", status, body)
			}
			wire := <-f.requests
			if !reflect.DeepEqual(ojson.GetBytes(wire, "input").Value(), doc.Get("input").Value()) {
				t.Errorf("Normal tool images changed: %s", wire)
			}
			t.Logf("source=%s wire=%s", doc.String(), wire)
			kiloM7BAssertRound(t, f, seen, first, 2)
		})
	}
}

func TestKiloM7BCallIDCollisionsAndDuplicates(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate=%t", duplicate), func(t *testing.T) {
			long := strings.Repeat("k", 80)
			ids := []string{long, "kilo_local_call_0", strings.Repeat("k", 79) + "z"}
			if duplicate {
				ids = []string{long, long}
			}
			f, seen := kiloM7BFixture(t, "function_call", ids[0], ids[1:]...)
			first := kiloM7BStart(t, f, seen, long)
			calls, messages := []any{}, []any{}
			for _, id := range ids {
				calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "source_tool", "arguments": "{}"}})
			}
			messages = append(messages, map[string]any{"role": "assistant", "tool_calls": calls})
			for i, id := range ids {
				messages = append(messages, map[string]any{"role": "tool", "tool_call_id": id, "content": fmt.Sprintf("result-%d", i)})
			}
			doc := ojson.NewDocString(`{"model":"gpt-6.1-sol"}`)
			_ = doc.Set("messages", messages)
			status, body := f.request(t, "POST", "/v1/chat/completions", "synthetic-kilo-key", doc.String())
			if status != 200 {
				t.Fatalf("HTTP=%d body=%s", status, body)
			}
			wire := <-f.requests
			gotCalls, gotOutputs := []string{}, map[string]string{}
			for _, item := range ojson.GetBytes(wire, "input").Array() {
				switch item.Get("type").String() {
				case "function_call":
					gotCalls = append(gotCalls, item.Get("call_id").String())
				case "function_call_output":
					id := item.Get("call_id").String()
					if _, exists := gotOutputs[id]; exists {
						t.Error("duplicate output")
					}
					gotOutputs[id] = item.Get("output").String()
				}
			}
			if duplicate {
				// The shared translator drops ambiguous calls; registered tool-only
				// recovery keeps the first result, matching the existing Kilo rule.
				if len(gotCalls) != 0 || len(gotOutputs) != 1 || gotOutputs[long] != "result-0" {
					t.Errorf("duplicate semantics changed: %s", wire)
				}
			} else {
				if !reflect.DeepEqual(gotCalls, ids) || len(gotOutputs) != len(ids) {
					t.Errorf("calls collided: %s", wire)
				}
				for i, id := range ids {
					if gotOutputs[id] != fmt.Sprintf("result-%d", i) {
						t.Errorf("output attribution changed: %s", wire)
					}
				}
			}
			t.Logf("source=%s wire=%s", doc.String(), wire)
			kiloM7BAssertRound(t, f, seen, first, 2)
		})
	}
}

func TestKiloM7BPayloadFinalBarrier(t *testing.T) {
	for _, history := range []bool{false, true} {
		for _, mode := range []string{"override-images", "delete-images", "override-id", "delete-id", "delete-tools"} {
			t.Run(fmt.Sprintf("history=%t/%s", history, mode), func(t *testing.T) {
				id := strings.Repeat("k", 80)
				f, seen := kiloM7BFixture(t, "function_call", id)
				first := kiloM7BStart(t, f, seen, id)
				source := ojson.NewDocString(kiloM7BContinuation("function_call", id, history, ojson.Parse(kiloM7BImages).Value()))
				messages := []any{map[string]any{"role": "user", "content": ojson.Parse(kiloM7BImages).Value()}}
				for _, message := range source.Get("messages").Array() {
					messages = append(messages, message.Value())
				}
				_ = source.Set("messages", messages)
				outputIndex := 1
				if history {
					outputIndex = 2
				}
				output := fmt.Sprintf("input.%d", outputIndex)
				next := f.cfg.CloneForRuntime()
				models := []config.PayloadModelRule{{Name: "*"}}
				params := map[string]any{}
				filter := []string{}
				switch mode {
				case "override-images":
					params["input.0.content.1.image_url"] = "https://synthetic.invalid/config.png"
					params["input.0.content.1.detail"] = "low"
					params[output+".output.1.image_url"] = "https://synthetic.invalid/config-tool.png"
					params[output+".output.1.detail"] = "auto"
				case "delete-images":
					filter = []string{"input.0.content.1", output + ".output.1"}
				case "override-id":
					params[output+".call_id"] = "unowned-config-id"
				case "delete-id":
					filter = []string{output + ".call_id"}
				case "delete-tools":
					params["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "configured-only"}}}}
				}
				next.Payload.Override = []config.PayloadRule{{Models: models, Params: params}}
				next.Payload.Filter = []config.PayloadFilterRule{{Models: models, Params: filter}}
				f.executor.UpdateConfig(next)
				status, body := f.request(t, "POST", "/v1/chat/completions", "synthetic-kilo-key", source.String())
				if mode == "override-id" {
					f.assertReleased(t, 2)
					if status != 409 || f.posts.Load() != 1 || f.hook.snapshot()[1].Success {
						t.Fatalf("unowned configured ID escaped barrier HTTP=%d POST=%d body=%s", status, f.posts.Load(), body)
					}
					t.Logf("configured unowned ID rejected HTTP=409 POST=1 released; public=%s", body)
					return
				}
				if status != 200 {
					t.Fatalf("HTTP=%d body=%s", status, body)
				}
				wire := <-f.requests
				switch mode {
				case "override-images":
					if ojson.GetBytes(wire, "input.0.content.1.image_url").String() != "https://synthetic.invalid/config.png" || ojson.GetBytes(wire, "input.0.content.1.detail").String() != "low" || ojson.GetBytes(wire, output+".output.1.image_url").String() != "https://synthetic.invalid/config-tool.png" || ojson.GetBytes(wire, output+".output.1.detail").String() != "auto" {
						t.Errorf("configured image restored: %s", wire)
					}
				case "delete-images":
					if bytes.Contains(wire, []byte("data:image/png")) || ojson.GetBytes(wire, "input.0.content.#").Int() != 2 || ojson.GetBytes(wire, output+".output.#").Int() != 2 {
						t.Errorf("deleted image restored: %s", wire)
					}
				case "delete-id":
					if ojson.GetBytes(wire, output+".call_id").Exists() {
						t.Errorf("deleted ID restored: %s", wire)
					}
				case "delete-tools":
					if bytes.Contains(wire, []byte("call_id")) || ojson.GetBytes(wire, "input.0.content.0.text").String() != "configured-only" || ojson.GetBytes(wire, "input.#").Int() != 1 {
						t.Errorf("deleted tools restored: %s", wire)
					}
				}
				t.Logf("source=%s wire=%s", source.String(), wire)
				kiloM7BAssertRound(t, f, seen, first, 2)
			})
		}
	}
}

func TestKiloM7BInterleavedSSEAndImages(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"m7b-sse","model":"openai/gpt-6.1-sol","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-m7b","type":"function_call","call_id":"call-m7b-fn","name":"lookup","arguments":""}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"ct-m7b","type":"custom_tool_call","call_id":"call-m7b-custom","name":"raw_tool","input":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc-m7b","output_index":0,"delta":"{\"query\":\"北"}`,
		`{"type":"response.custom_tool_call_input.delta","output_index":1,"delta":"补丁 "}`,
		`{"type":"synthetic.success_extension","preserved":{"exact":"未知成功事件"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc-m7b","output_index":0,"delta":"京\"}"}`,
		`{"type":"response.custom_tool_call_input.delta","item_id":"ct-m7b","output_index":1,"delta":"λ 完成"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc-m7b","output_index":0,"arguments":"{\"query\":\"北京\"}"}`,
		`{"type":"response.custom_tool_call_input.done","item_id":"ct-m7b","output_index":1,"input":"补丁 λ 完成"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"fc-m7b","type":"function_call","call_id":"call-m7b-fn","name":"lookup","arguments":"{\"query\":\"北京\"}"}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"ct-m7b","type":"custom_tool_call","call_id":"call-m7b-custom","name":"raw_tool","input":"补丁 λ 完成"}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"ct-m7b","type":"custom_tool_call","call_id":"call-m7b-custom","name":"raw_tool","input":"补丁 λ 完成"}}`,
		`{"type":"response.image_generation_call.partial_image","item_id":"ig-m7b","output_format":"png","partial_image_b64":"c3ludGhldGlj","partial_image_index":0}`,
		`{"type":"response.output_item.done","item":{"id":"ig-m7b","type":"image_generation_call","output_format":"png","result":"c3ludGhldGlj"}}`,
		`{"type":"response.output_item.done","item":{"id":"ig-m7b","type":"image_generation_call","output_format":"jpeg","result":"ZmluYWw="}}`,
		`{"type":"response.output_text.delta","delta":"tail-after-done"}`,
		`{"type":"response.completed","response":{"id":"m7b-sse","model":"openai/gpt-6.1-sol","status":"completed","output":[{"type":"function_call","id":"fc-m7b","call_id":"call-m7b-fn","name":"lookup","arguments":"{\"query\":\"北京\"}"},{"type":"custom_tool_call","id":"ct-m7b","call_id":"call-m7b-custom","name":"raw_tool","input":"补丁 λ 完成"},{"type":"image_generation_call","id":"ig-m7b","output_format":"jpeg","result":"ZmluYWw="}]}}`,
	}
	for _, protocol := range []string{"responses", "chat/completions"} {
		for _, stream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/stream=%t", protocol, stream), func(t *testing.T) {
				seen := make(chan kiloSessionSeen, 2)
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					kiloM7BSeen(t, r, seen)
					w.Header().Set("Content-Type", "text/event-stream")
					for _, event := range events {
						_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
						w.(http.Flusher).Flush()
					}
				})
				status, body := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"test","messages":[{"role":"user","content":"test"}],"stream":%t}`, stream))
				if status != 200 {
					t.Fatalf("HTTP=%d body=%s", status, body)
				}
				t.Logf("public=%s", body)
				if protocol == "responses" && stream {
					for _, event := range events {
						want := strings.ReplaceAll(event, `"model":"openai/gpt-6.1-sol"`, `"model":"gpt-6.1-sol"`)
						if !bytes.Contains(body, []byte(want)) {
							t.Errorf("Normal lost event: %s", want)
						}
					}
				} else if protocol == "responses" {
					if ojson.GetBytes(body, "output.2.result").String() != "ZmluYWw=" {
						t.Error("Normal image result lost")
					}
				} else if !stream {
					if ojson.GetBytes(body, "choices.0.message.images.0.image_url.url").String() != "data:image/jpeg;base64,ZmluYWw=" || ojson.GetBytes(body, "choices.0.message.tool_calls.1.function.arguments").String() != "补丁 λ 完成" {
						t.Error("nonstream tool/image result differs")
					}
				} else {
					ids, arguments, names := map[int]string{}, map[int]string{}, map[int]string{}
					images := []string{}
					tail, finishes := false, 0
					for _, line := range bytes.Split(body, []byte("\n")) {
						if !bytes.HasPrefix(line, []byte("data:")) {
							continue
						}
						chunk := ojson.ParseBytes(bytes.TrimSpace(line[5:]))
						if chunk.Get("choices.0.delta.content").String() == "tail-after-done" {
							tail = true
						}
						if finish := chunk.Get("choices.0.finish_reason"); finish.Type == ojson.String {
							finishes++
							if !tail || finish.String() != "tool_calls" {
								t.Errorf("early/wrong finish: %s", chunk.Raw)
							}
						}
						for _, call := range chunk.Get("choices.0.delta.tool_calls").Array() {
							index := int(call.Get("index").Int())
							ids[index] += call.Get("id").String()
							names[index] += call.Get("function.name").String()
							arguments[index] += call.Get("function.arguments").String()
						}
						for _, img := range chunk.Get("choices.0.delta.images").Array() {
							images = append(images, img.Get("image_url.url").String())
						}
					}
					if len(ids) != 2 || ids[0] != "call-m7b-fn" || ids[1] != "call-m7b-custom" || names[0] != "lookup" || names[1] != "raw_tool" || arguments[0] != `{"query":"北京"}` || arguments[1] != "补丁 λ 完成" || finishes != 1 {
						t.Errorf("SSE identity/arguments/finish differs: ids=%v names=%v arguments=%v finishes=%d", ids, names, arguments, finishes)
					}
					if !reflect.DeepEqual(images, []string{"data:image/png;base64,c3ludGhldGlj", "data:image/jpeg;base64,ZmluYWw="}) {
						t.Errorf("image partial/result differs: %v", images)
					}
				}
				owner := <-seen
				seen <- owner
				kiloM7BAssertRound(t, f, seen, owner, 1)
			})
		}
	}
}
