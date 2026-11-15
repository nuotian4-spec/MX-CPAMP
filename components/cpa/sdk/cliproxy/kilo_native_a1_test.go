package cliproxy

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
)

func kiloA1Request(t *testing.T, f *kiloNativeFixture, key, thread, parent, initialization, raw string) (int, []byte) {
	t.Helper()
	doc := ojson.NewDocString(raw)
	_ = doc.Set("model", "gpt-6.1-sol")
	_ = doc.Set("prompt_cache_key", "root")
	meta := ojson.NewDocString(`{}`)
	_ = meta.Set("thread_id", thread)
	_ = meta.Set("session_id", "root")
	_ = meta.Set("history_initialization", initialization)
	if parent != "" {
		_ = meta.Set("parent_thread_id", parent)
	}
	_ = doc.Set("client_metadata.thread_id", thread)
	_ = doc.Set("client_metadata.session_id", "root")
	_ = doc.Set("client_metadata.x-codex-turn-metadata", meta.String())
	r, err := http.NewRequest("POST", f.server.URL+"/v1/responses", strings.NewReader(doc.String()))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Thread-Id", thread)
	r.Header.Set("Session-Id", "root")
	r.Header.Set("X-Codex-Turn-Metadata", meta.String())
	response, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body
}

func kiloA1Event(w http.ResponseWriter, event string) {
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ojson.Get(event, "type").String(), event)
	w.(http.Flusher).Flush()
}

func kiloA1Terminal(w http.ResponseWriter, id string, output any) {
	doc := ojson.NewDocString(kiloTerminal)
	_ = doc.Set("response.id", id)
	if output != nil {
		_ = doc.Set("response.output", output)
	}
	kiloA1Event(w, doc.String())
}

func TestKiloA1NativeToolSearchAndOwnership(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var posts atomic.Int32
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				step := posts.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if step == 1 {
					if ojson.GetBytes(body, "tools.0.name").String() != helps.KiloToolSearchFunction || ojson.GetBytes(body, "tool_choice.name").String() != helps.KiloToolSearchFunction {
						t.Errorf("native search was not adapted: %s", body)
					}
					call := ojson.NewDocString(`{"type":"function_call","id":"fc-search","call_id":"search-call","name":"__kilo2api_tool_search","arguments":"{","status":"in_progress"}`)
					kiloA1Event(w, `{"type":"response.created","response":{"id":"resp-search","status":"in_progress"}}`)
					kiloA1Event(w, `{"type":"response.output_item.added","output_index":0,"item":`+call.String()+`}`)
					kiloA1Event(w, `{"type":"response.function_call_arguments.delta","item_id":"fc-search","delta":"{"}`)
					_ = call.Set("arguments", `{"query":"fixture"}`)
					_ = call.Set("status", "completed")
					kiloA1Event(w, `{"type":"response.output_item.done","output_index":0,"item":`+call.String()+`}`)
					kiloA1Terminal(w, "resp-search", []any{ojson.ParseBytes(call.Bytes()).Value()})
					return
				}
				if ojson.GetBytes(body, "input.0.name").String() != helps.KiloToolSearchFunction || ojson.GetBytes(body, "input.2.type").String() != "additional_tools" || strings.Contains(string(body), `"defer_loading"`) {
					t.Errorf("loaded history is not reversible: %s", body)
				}
				kiloA1Terminal(w, "resp-after-search", nil)
			})
			definition := `{"type":"tool_search","execution":"client","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}`
			code, response := kiloA1Request(t, f, "synthetic-kilo-key", "root", "", "new", fmt.Sprintf(`{"input":"discover","stream":%t,"tools":[%s],"tool_choice":{"type":"tool_search"}}`, stream, definition))
			if code != 200 || !strings.Contains(string(response), `"tool_search_call"`) || strings.Contains(string(response), helps.KiloToolSearchFunction) || strings.Contains(string(response), "function_call_arguments") {
				t.Fatalf("search response=%d %s", code, response)
			}
			f.assertReleased(t, 1)
			pair := `[{"type":"tool_search_call","execution":"client","call_id":"search-call","arguments":{"query":"fixture"}},{"type":"tool_search_output","execution":"client","call_id":"search-call","tools":[{"type":"namespace","name":"fixture","tools":[{"type":"function","name":"echo","defer_loading":true,"parameters":{"type":"object"}}]}]}]`
			code, response = kiloA1Request(t, f, "synthetic-kilo-key", "root", "", "new", fmt.Sprintf(`{"input":%s,"tools":[%s],"stream":%t}`, pair, definition, stream))
			if code != 200 || !strings.Contains(string(response), "kilo-ok") {
				t.Fatalf("search continuation=%d %s", code, response)
			}
			f.assertReleased(t, 2)
			code, response = kiloA1Request(t, f, "synthetic-kilo-key", "root", "", "new", fmt.Sprintf(`{"input":[{"type":"tool_search_output","execution":"client","call_id":"search-call","tools":[]}],"tools":[%s]}`, definition))
			if code != 400 {
				t.Fatalf("output-only=%d %s", code, response)
			}
			if f.posts.Load() != 2 {
				t.Fatal("rejected history was sent or replayed")
			}
			f.assertReleased(t, 3)
			files, _ := os.ReadDir(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings"))
			found := false
			for _, file := range files {
				raw, _ := os.ReadFile(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings", file.Name()))
				for _, record := range ojson.GetBytes(raw, "bindings").Array() {
					found = found || record.Get("call_types.search-call").String() == "tool_search"
				}
			}
			if !found {
				t.Fatal("public search kind was not persisted")
			}
			t.Logf("native search stream=%t: POST=2 public kind=tool_search, paired continuation accepted, orphan rejected, resources=0", stream)
		})
	}
}

func TestKiloA1NativeBranchesAndRestart(t *testing.T) {
	seen := make(chan kiloSessionSeen, 16)
	f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
	f.requests = make(chan []byte, 32)
	pair := func(id string) string {
		return fmt.Sprintf(`[{"type":"function_call","namespace":"collaboration-optimize","name":"spawn_agent","call_id":%q,"arguments":"{}"},{"type":"function_call_output","call_id":%q,"output":"aborted"}]`, id, id)
	}
	request := func(thread, parent, mode, raw string, want int) {
		t.Helper()
		code, response := kiloA1Request(t, f, "synthetic-kilo-key", thread, parent, mode, raw)
		if code != want {
			t.Fatalf("thread=%s expected=%d got=%d %s", thread, want, code, response)
		}
	}
	request("root", "", "new", `{"input":"parent"}`, 200)
	parent := <-seen
	request("child", "root", "warm_fork", `{"input":`+pair("call-resp-chain-1")+`}`, 200)
	child := <-seen
	if child.Account != parent.Account || child.Session == parent.Session {
		t.Fatalf("full branch shared session or moved identity: parent=%+v child=%+v", parent, child)
	}
	request("root", "", "new", `{"input":"parent future"}`, 200)
	if next := <-seen; next != parent {
		t.Fatal("parent alias moved")
	}
	request("fresh", "missing-control-parent", "new", `{"input":"fresh task"}`, 200)
	fresh := <-seen
	if fresh.Session == child.Session || fresh.Session == parent.Session {
		t.Fatal("fresh child shared upstream session")
	}
	for _, id := range []string{"call-resp-chain-3", "call-resp-chain-4"} {
		request("child", "root", "warm_fork", `{"input":`+pair(id)+`}`, 409)
	}
	request("child", "root", "warm_fork", `{"input":[{"type":"function_call_output","call_id":"call-resp-chain-1","output":"new unauthorized result"}]}`, 409)
	request("new-full", "root", "warm_fork", `{"previous_response_id":"resp-chain-1","input":"fork"}`, 409)
	request("child", "root", "warm_fork", `{"input":`+pair("call-resp-chain-1")+`}`, 200)
	if next := <-seen; next != child {
		t.Fatal("child continuation moved")
	}
	f.assertReleased(t, 5)
	restartKiloSessionFixture(t, f)
	request("child", "root", "warm_fork", `{"input":`+pair("call-resp-chain-1")+`}`, 200)
	if next := <-seen; next != child {
		t.Fatal("restart lost child branch")
	}
	f.assertReleased(t, 1)
	dir := filepath.Join(config.KiloStateDirectory(f.cfg), "bindings")
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		path := filepath.Join(dir, file.Name())
		raw, _ := os.ReadFile(path)
		doc := ojson.NewDoc(raw)
		for i, record := range doc.Get("bindings").Array() {
			if record.Get("upstream_session").String() == parent.Session {
				if record.Get("sessions").Raw != `["root"]` {
					t.Fatalf("parent aliases=%s", record.Get("sessions").Raw)
				}
				_ = doc.Set(fmt.Sprintf("bindings.%d.expires_at", i), time.Unix(1, 0).UTC().Format(time.RFC3339Nano))
			}
		}
		if err := os.WriteFile(path, doc.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	request("child", "root", "warm_fork", `{"input":`+pair("call-resp-chain-1")+`}`, 409)
	if f.posts.Load() != 6 {
		t.Fatalf("unexpected POST count=%d", f.posts.Load())
	}
	f.assertReleased(t, 1)
	t.Log("formal Router/AuthManager/Executor: parent/full/fresh independent; inherited snapshot fixed; future/sibling/orphan/expired rejected; restart retained branch; all resources released")
}

func TestKiloA1PublishFailureSuppressesToolFrame(t *testing.T) {
	var f *kiloNativeFixture
	f = newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		dir := filepath.Join(config.KiloStateDirectory(f.cfg), "bindings")
		files, _ := os.ReadDir(dir)
		for _, file := range files {
			if err := os.WriteFile(filepath.Join(dir, file.Name()), []byte(`{`), 0600); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		kiloA1Event(w, `{"type":"response.output_item.added","item":{"type":"function_call","id":"fc-fail","name":"__kilo2api_tool_search","call_id":"unpublished","arguments":"","status":"in_progress"}}`)
		kiloA1Terminal(w, "never-public", nil)
	})
	code, body := kiloA1Request(t, f, "synthetic-kilo-key", "root", "", "new", `{"stream":true,"input":"search","tools":[{"type":"tool_search","execution":"client","parameters":{"type":"object"}}]}`)
	if code == 200 || strings.Contains(string(body), "unpublished") || strings.Contains(string(body), "tool_search_call") {
		t.Fatalf("unregistered tool exposed: %d %s", code, body)
	}
	if f.posts.Load() != 1 {
		t.Fatal("storage failure replayed")
	}
	f.assertReleased(t, 1)
}

func TestKiloA1FinalPayloadBarrier(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream)
	next := *f.cfg
	next.Payload.Filter = []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: []string{"tools", "tool_choice"}}}
	f.executor.UpdateConfig(&next)
	f.manager.SetConfig(&next)
	before := helps.PayloadConfigEvaluationCount()
	code, body := kiloA1Request(t, f, "synthetic-kilo-key", "root", "", "new", `{"input":"search","tools":[{"type":"tool_search","execution":"client","parameters":{"type":"object"}}],"tool_choice":{"type":"tool_search"}}`)
	if code != 200 {
		t.Fatalf("filtered request=%d %s", code, body)
	}
	wire := <-f.requests
	if ojson.GetBytes(wire, "tools").Exists() || ojson.GetBytes(wire, "tool_choice").Exists() || helps.PayloadConfigEvaluationCount()-before != 1 {
		t.Fatal("tool search restored filtered fields or evaluated payload rules more than once")
	}
	f.assertReleased(t, 1)
}
