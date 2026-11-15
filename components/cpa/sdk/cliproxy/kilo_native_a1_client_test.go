package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func kiloA1String(value string) string {
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("value", value)
	return doc.Get("value").Raw
}

func kiloA1Message(id, value string) any {
	return map[string]any{"type": "message", "id": id, "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": value}}}
}

func kiloA1Call(id, name string, arguments any) any {
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("value", arguments)
	return map[string]any{"type": "function_call", "id": "fc-" + id, "call_id": "call-" + id, "name": name, "namespace": "collaboration-optimize", "arguments": doc.Get("value").Raw}
}

func kiloA1ClientOutput(w http.ResponseWriter, id string, items []any) {
	w.Header().Set("Content-Type", "text/event-stream")
	kiloA1Event(w, fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"status":"in_progress","output":[],"model":"openai/gpt-6.1-sol"}}`, id))
	for index, item := range items {
		for _, kind := range []string{"response.output_item.added", "response.output_item.done"} {
			doc := ojson.NewDocString(`{}`)
			_ = doc.Set("type", kind)
			_ = doc.Set("output_index", index)
			_ = doc.Set("item", item)
			kiloA1Event(w, doc.String())
		}
	}
	kiloA1Terminal(w, id, items)
}

func TestKiloA1RealClientBranches(t *testing.T) {
	binary := os.Getenv("KILO_A1_CLIENT_BINARY")
	if binary == "" {
		t.Skip("set KILO_A1_CLIENT_BINARY for installed-client acceptance")
	}
	c0 := os.Getenv("KILO_A1_C0_EVIDENCE")
	disable, err := os.ReadFile(filepath.Join(c0, "skills-disable-config.txt"))
	if err != nil {
		t.Fatal(err)
	}
	catalogBytes, err := os.ReadFile(filepath.Join(c0, "synthetic-model-catalog-no-search.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"none", "all", "search"} {
		t.Run(mode, func(t *testing.T) {
			rootDir := os.Getenv("KILO_A1_EVIDENCE_DIR")
			if rootDir == "" {
				rootDir = t.TempDir()
			}
			dir := filepath.Join(rootDir, "real-client-"+mode)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			home, work := filepath.Join(dir, "home"), filepath.Join(dir, "workspace")
			for _, path := range []string{home, work, filepath.Join(home, "appdata"), filepath.Join(home, "localappdata"), filepath.Join(home, "temp")} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			catalog := ojson.NewDoc(catalogBytes)
			_ = catalog.Set("models.0.slug", "gpt-6.1-sol")
			if mode == "search" {
				_ = catalog.Set("models.0.supports_search_tool", true)
			}
			catalogPath := filepath.Join(dir, "synthetic-model-catalog.json")
			if err := os.WriteFile(catalogPath, catalog.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var rootThread string
			parentSteps := 0
			var wires, inbound []any
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				thread := ojson.GetBytes(raw, "client_metadata.thread_id").String()
				mu.Lock()
				if rootThread == "" {
					rootThread = thread
				}
				child := thread != rootThread
				step := parentSteps
				if !child {
					parentSteps++
				}
				wires = append(wires, map[string]any{"thread_id": thread, "upstream_session": r.Header.Get("X-Session-Id"), "machine": r.Header.Get("X-KILOCODE-MACHINEID"), "body": ojson.ParseBytes(raw).Value()})
				mu.Unlock()
				if mode == "search" {
					if step == 0 {
						kiloA1ClientOutput(w, "resp-a1-search", []any{map[string]any{"type": "function_call", "id": "fc-a1-search", "call_id": "call-a1-search", "name": "__kilo2api_tool_search", "arguments": `{"query":"c0_echo synthetic offline marker","limit":1}`}})
					} else {
						kiloA1ClientOutput(w, "resp-a1-search-final", []any{kiloA1Message("msg-search-final", "A1_SEARCH_COMPLETE")})
					}
				} else if child {
					kiloA1ClientOutput(w, "resp-a1-child", []any{kiloA1Message("msg-child", "A1_CHILD_RESULT_"+mode)})
				} else if step == 0 {
					kiloA1ClientOutput(w, "resp-a1-spawn", []any{kiloA1Message("msg-parent-history", "A1_PARENT_HISTORY"), kiloA1Call("a1-spawn", "spawn_agent", map[string]any{"task_name": "a1_child", "message": "A1_CHILD_TASK synthetic offline fixture; return the fixed child result without tools.", "fork_turns": mode})})
				} else if step == 1 {
					kiloA1ClientOutput(w, "resp-a1-wait", []any{kiloA1Call("a1-wait", "wait_agent", map[string]any{"timeout_ms": 10000})})
				} else {
					kiloA1ClientOutput(w, "resp-a1-parent-final", []any{kiloA1Message("msg-parent-final", "A1_PARENT_CONSUMED_CHILD")})
				}
			})
			original := f.server.Config.Handler
			f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
				headers := r.Header.Clone()
				headers.Del("Authorization")
				mu.Lock()
				inbound = append(inbound, map[string]any{"path": r.URL.Path, "headers": headers, "synthetic_authorization": r.Header.Get("Authorization") == "Bearer synthetic-kilo-key", "body": ojson.ParseBytes(raw).Value()})
				mu.Unlock()
				original.ServeHTTP(w, r)
			})
			var blocked atomic.Int32
			deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				blocked.Add(1)
				http.Error(w, "offline fixture deny", 403)
			}))
			defer deny.Close()
			configs := []string{`model="gpt-6.1-sol"`, `model_provider="fixture"`, `model_providers.fixture.name="OpenAI"`, `model_providers.fixture.wire_api="responses"`, `model_providers.fixture.supports_websockets=false`, `model_providers.fixture.requires_openai_auth=false`, `model_providers.fixture.env_key="OPENAI_API_KEY"`, `model_providers.fixture.request_max_retries=0`, "model_providers.fixture.base_url=" + kiloA1String(f.server.URL+"/v1"), "model_catalog_json=" + kiloA1String(catalogPath), `features.multi_agent_v2.enabled=true`, `features.multi_agent_v2.tool_namespace="collaboration-optimize"`, `features.skip_host_skill_discovery=true`, `model_reasoning_effort="low"`, `web_search="disabled"`, `project_doc_max_bytes=0`, string(disable)}
			if mode == "search" {
				python := os.Getenv("KILO_A1_PYTHON")
				if python == "" {
					t.Fatal("set KILO_A1_PYTHON for the synthetic stdio MCP fixture")
				}
				configs = append(configs, `features.multi_agent_v2_dynamic_tools=true`, "mcp_servers.c0fixture.command="+kiloA1String(python), "mcp_servers.c0fixture.args=["+kiloA1String(filepath.Join(c0, "mcp_fixture.py"))+","+kiloA1String(filepath.Join(dir, "mcp-transcript.jsonl"))+"]")
			}
			args := []string{"exec", "--ignore-user-config", "--ignore-rules", "--json", "--color", "never", "--skip-git-repo-check", "-C", work, "-s", "read-only"}
			for _, value := range configs {
				args = append(args, "-c", value)
			}
			args = append(args, "A1_PARENT_MARKER synthetic local contract fixture; follow supplied calls without reading files or running commands.")
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Dir = work
			for _, name := range []string{"SystemRoot", "WINDIR", "PATH", "PATHEXT", "COMSPEC", "PROCESSOR_ARCHITECTURE", "NUMBER_OF_PROCESSORS"} {
				cmd.Env = append(cmd.Env, name+"="+os.Getenv(name))
			}
			cmd.Env = append(cmd.Env, "CODEX_HOME="+home, "HOME="+home, "USERPROFILE="+home, "APPDATA="+filepath.Join(home, "appdata"), "LOCALAPPDATA="+filepath.Join(home, "localappdata"), "TEMP="+filepath.Join(home, "temp"), "TMP="+filepath.Join(home, "temp"), "OPENAI_API_KEY=synthetic-kilo-key", "OPENAI_BASE_URL="+f.server.URL+"/v1", "RUST_LOG=error", "OTEL_SDK_DISABLED=true", "HTTP_PROXY="+deny.URL, "HTTPS_PROXY="+deny.URL, "ALL_PROXY="+deny.URL, "http_proxy="+deny.URL, "https_proxy="+deny.URL, "all_proxy="+deny.URL, "NO_PROXY=127.0.0.1,localhost,::1", "no_proxy=127.0.0.1,localhost,::1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			started := time.Now().Format(time.RFC3339Nano)
			runErr := cmd.Run()
			_ = os.WriteFile(filepath.Join(dir, "client.stdout.jsonl"), stdout.Bytes(), 0600)
			_ = os.WriteFile(filepath.Join(dir, "client.stderr.txt"), stderr.Bytes(), 0600)
			mu.Lock()
			doc := ojson.NewDocString(`{}`)
			_ = doc.Set("binary", binary)
			_ = doc.Set("arguments", args)
			_ = doc.Set("started_at", started)
			_ = doc.Set("finished_at", time.Now().Format(time.RFC3339Nano))
			exitCode := -1
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			}
			_ = doc.Set("exit_code", exitCode)
			_ = doc.Set("incoming", inbound)
			_ = doc.Set("upstream", wires)
			_ = doc.Set("blocked_proxy_requests", blocked.Load())
			_ = doc.Set("os_network_sandbox", false)
			_ = os.WriteFile(filepath.Join(dir, "client-contract.json"), doc.Bytes(), 0600)
			mu.Unlock()
			if runErr != nil {
				t.Fatalf("client failed: %v stderr=%s stdout=%s", runErr, stderr.String(), stdout.String())
			}
			marker := "A1_PARENT_CONSUMED_CHILD"
			if mode == "search" {
				marker = "A1_SEARCH_COMPLETE"
			}
			if !strings.Contains(stdout.String(), `"type":"turn.completed"`) || !strings.Contains(stdout.String(), marker) {
				t.Fatalf("client did not finish: %s", stdout.String())
			}
			if mode == "search" {
				incoming := doc.Get("incoming").Array()
				upstream := doc.Get("upstream").Array()
				if len(incoming) != 2 || len(upstream) != 2 || f.posts.Load() != 2 {
					t.Fatalf("search POST/replay count: incoming=%d upstream=%d posts=%d", len(incoming), len(upstream), f.posts.Load())
				}
				call, output, loaded := false, false, false
				for _, item := range incoming[1].Get("body.input").Array() {
					if item.Get("call_id").String() != "call-a1-search" {
						continue
					}
					call = call || item.Get("type").String() == "tool_search_call"
					output = output || (item.Get("type").String() == "tool_search_output" && strings.Contains(item.Raw, "mcp__c0fixture") && strings.Contains(item.Raw, `"defer_loading":true`))
				}
				for _, item := range upstream[1].Get("body.input").Array() {
					loaded = loaded || (item.Get("type").String() == "additional_tools" && strings.Contains(item.Raw, "c0_echo") && !strings.Contains(item.Raw, "defer_loading"))
				}
				if !call || !output || !loaded || !strings.Contains(upstream[1].Get("body.input").Raw, "__kilo2api_tool_search") {
					t.Fatalf("native search contract: call=%t output=%t loaded=%t", call, output, loaded)
				}
				f.assertReleased(t, 2)
				t.Log("installed client search: native paired call/output, namespace/defer_loading preserved, bridge additional_tools loaded, POST=2 resources=0; no MCP tool execution requested")
				return
			}
			var parentSession, childSession, parentMachine, childMachine, childThread string
			returned := false
			for _, item := range doc.Get("upstream").Array() {
				thread := item.Get("thread_id").String()
				if thread == rootThread {
					if parentSession != "" && parentSession != item.Get("upstream_session").String() {
						t.Fatal("parent upstream session moved")
					}
					parentSession, parentMachine = item.Get("upstream_session").String(), item.Get("machine").String()
					returned = returned || strings.Contains(item.Get("body.input").Raw, "A1_CHILD_RESULT_"+mode)
				} else {
					childThread, childSession, childMachine = thread, item.Get("upstream_session").String(), item.Get("machine").String()
				}
			}
			if childThread == "" || childSession == "" || childSession == parentSession || !returned {
				t.Fatalf("branch/result contract missing: root=%s child=%s parentSession=%s childSession=%s returned=%t", rootThread, childThread, parentSession, childSession, returned)
			}
			if mode == "all" && childMachine != parentMachine {
				t.Fatal("full child moved identity")
			}
			if f.posts.Load() != 4 {
				t.Fatalf("unexpected POST/replay count=%d", f.posts.Load())
			}
			f.assertReleased(t, 4)
			t.Logf("installed client mode=%s root=%s child=%s POST=4 parent_session=%s child_session=%s result_returned=true resources=0", mode, rootThread, childThread, parentSession, childSession)
		})
	}
}
