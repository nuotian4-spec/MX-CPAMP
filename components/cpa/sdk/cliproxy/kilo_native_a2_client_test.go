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

func TestKiloSearchA2RealClientFormalAlphaAndFollowup(t *testing.T) {
	binary := os.Getenv("KILO_A2_CLIENT_BINARY")
	if binary == "" {
		t.Fatal("set KILO_A2_CLIENT_BINARY for installed-client acceptance")
	}
	disable, err := os.ReadFile(filepath.Join(os.Getenv("KILO_A2_C0_EVIDENCE"), "skills-disable-config.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"alpha", "followup"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(os.Getenv("KILO_A2_EVIDENCE_DIR"), "real-client-"+mode+os.Getenv("KILO_A2_CLIENT_SUFFIX"))
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			home, work := filepath.Join(dir, "home"), filepath.Join(dir, "workspace")
			for _, path := range []string{home, work, filepath.Join(home, "appdata"), filepath.Join(home, "localappdata"), filepath.Join(home, "temp")} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var rootThread, childThread string
			parentSteps, childSteps := 0, 0
			followupSent := false
			var wires, inbound []any
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				thread := ojson.GetBytes(raw, "client_metadata.thread_id").String()
				alpha := ojson.GetBytes(raw, "tools.0.type").String() == "web_search"
				mu.Lock()
				if rootThread == "" && !alpha {
					rootThread = thread
				}
				child := thread != "" && thread != rootThread
				step := parentSteps
				if child {
					childSteps++
					if childThread != "" && childThread != thread {
						t.Error("followup created a replacement child")
					}
					childThread = thread
				} else if !alpha {
					parentSteps++
				}
				childStep := childSteps
				wires = append(wires, map[string]any{"thread_id": thread, "alpha": alpha, "upstream_session": r.Header.Get("X-Session-Id"), "machine": r.Header.Get("X-KILOCODE-MACHINEID"), "body": ojson.ParseBytes(raw).Value()})
				mu.Unlock()
				if alpha {
					kiloA1ClientOutput(w, "resp-a2-alpha", []any{map[string]any{"type": "web_search_call", "id": "ws-a2-alpha", "status": "completed", "action": map[string]any{"type": "search", "sources": []any{map[string]any{"url": "https://example.invalid/a2-source", "title": "A2 synthetic source"}}}}, kiloA1Message("msg-a2-alpha", "A2_ALPHA_SOURCE")})
				} else if mode == "alpha" {
					if step == 0 {
						call := ojson.NewDocString(`{"type":"function_call","id":"fc-a2-web","call_id":"call-a2-web","name":"run","namespace":"web"}`)
						_ = call.Set("arguments", `{"search_query":[{"q":"A2 synthetic keyword","domains":["example.invalid"]}],"response_length":"short"}`)
						kiloA1ClientOutput(w, "resp-a2-web", []any{ojson.ParseBytes(call.Bytes()).Value()})
					} else {
						kiloA1ClientOutput(w, "resp-a2-web-final", []any{kiloA1Message("msg-a2-final", "A2_ALPHA_CONSUMED")})
					}
				} else if child {
					marker := "A2_CHILD_INITIAL"
					if childStep > 1 {
						marker = "A2_CHILD_FOLLOWUP"
					}
					kiloA1ClientOutput(w, fmt.Sprintf("resp-a2-child-%d", childStep), []any{kiloA1Message(fmt.Sprintf("msg-child-%d", childStep), marker)})
				} else if step == 0 {
					kiloA1ClientOutput(w, "resp-a2-spawn", []any{kiloA1Call("a2-spawn", "spawn_agent", map[string]any{"task_name": "a2_child", "message": "A2_CHILD_TASK synthetic fixture; return fixed output without tools.", "fork_turns": "none"})})
				} else if strings.Contains(string(raw), "A2_CHILD_FOLLOWUP") {
					kiloA1ClientOutput(w, "resp-a2-followup-final", []any{kiloA1Message("msg-a2-parent-final", "A2_PARENT_CONSUMED_FOLLOWUP")})
				} else if strings.Contains(string(raw), "A2_CHILD_INITIAL") && !followupSent {
					followupSent = true
					kiloA1ClientOutput(w, "resp-a2-followup", []any{kiloA1Call("a2-followup", "followup_task", map[string]any{"target": "a2_child", "message": "A2_FOLLOWUP_TASK: continue this same child and return fixed followup result without tools."})})
				} else {
					kiloA1ClientOutput(w, fmt.Sprintf("resp-a2-wait-%d", step), []any{kiloA1Call(fmt.Sprintf("a2-wait-%d", step), "wait_agent", map[string]any{"timeout_ms": 10000})})
				}
			})
			f.requests = make(chan []byte, 32)
			original := f.server.Config.Handler
			f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(raw))
				headers := r.Header.Clone()
				headers.Del("Authorization")
				mu.Lock()
				inbound = append(inbound, map[string]any{"method": r.Method, "path": r.URL.RequestURI(), "headers": headers, "synthetic_authorization": r.Header.Get("Authorization") == "Bearer synthetic-kilo-key", "body": ojson.ParseBytes(raw).Value()})
				mu.Unlock()
				original.ServeHTTP(w, r)
			})
			catalogStatus, catalogBody := f.request(t, "GET", "/v1/models?client_version=0.169.0-alpha.11.1", "synthetic-kilo-key", "")
			if catalogStatus != 200 {
				t.Fatalf("formal catalog=%d %s", catalogStatus, catalogBody)
			}
			found := false
			for _, model := range ojson.GetBytes(catalogBody, "models").Array() {
				if model.Get("slug").String() == "gpt-6.1-sol" {
					found = model.Get("supports_search_tool").Bool()
				}
			}
			if !found {
				t.Fatal("formal authenticated catalog did not publish the actual Kilo tool capability")
			}
			catalogPath := filepath.Join(dir, "formal-authenticated-model-catalog.json")
			if err := os.WriteFile(catalogPath, catalogBody, 0600); err != nil {
				t.Fatal(err)
			}
			var blocked atomic.Int32
			deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				blocked.Add(1)
				http.Error(w, "offline fixture deny", 403)
			}))
			defer deny.Close()
			configs := []string{`model="gpt-6.1-sol"`, `model_provider="fixture"`, `model_providers.fixture.name="OpenAI"`, `model_providers.fixture.wire_api="responses"`, `model_providers.fixture.supports_websockets=false`, `model_providers.fixture.requires_openai_auth=false`, `model_providers.fixture.env_key="OPENAI_API_KEY"`, `model_providers.fixture.request_max_retries=0`, "model_providers.fixture.base_url=" + kiloA1String(f.server.URL+"/v1"), `features.multi_agent_v2.enabled=true`, `features.multi_agent_v2.tool_namespace="collaboration-optimize"`, `features.skip_host_skill_discovery=true`, `model_reasoning_effort="low"`, `project_doc_max_bytes=0`, string(disable)}
			configs = append(configs, "model_catalog_json="+kiloA1String(catalogPath))
			if mode == "alpha" {
				configs = append(configs, `features.standalone_web_search=true`)
			} else {
				configs = append(configs, `web_search="disabled"`)
			}
			args := []string{"exec", "--ignore-user-config", "--ignore-rules", "--json", "--color", "never", "--skip-git-repo-check", "-C", work, "-s", "read-only"}
			for _, value := range configs {
				args = append(args, "-c", value)
			}
			args = append(args, "A2_PARENT_MARKER synthetic loopback fixture; follow supplied calls without reading files or running commands.")
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
			exit := -1
			if cmd.ProcessState != nil {
				exit = cmd.ProcessState.ExitCode()
			}
			_ = doc.Set("exit_code", exit)
			_ = doc.Set("incoming", inbound)
			_ = doc.Set("upstream", wires)
			_ = doc.Set("blocked_proxy_requests", blocked.Load())
			_ = doc.Set("os_network_sandbox", false)
			_ = doc.Set("catalog_mode", "formal-authenticated-response-snapshot")
			_ = doc.Set("catalog_path", catalogPath)
			_ = doc.Set("automatic_client_catalog_get", false)
			_ = os.WriteFile(filepath.Join(dir, "client-contract.json"), doc.Bytes(), 0600)
			mu.Unlock()
			if runErr != nil {
				t.Fatalf("client=%v stderr=%s stdout=%s", runErr, stderr.String(), stdout.String())
			}
			marker := "A2_PARENT_CONSUMED_FOLLOWUP"
			if mode == "alpha" {
				marker = "A2_ALPHA_CONSUMED"
			}
			if !strings.Contains(stdout.String(), `"type":"turn.completed"`) || !strings.Contains(stdout.String(), marker) {
				t.Fatalf("client did not complete: %s", stdout.String())
			}
			catalogFetch, alphaCalls := 0, 0
			for _, item := range doc.Get("incoming").Array() {
				if item.Get("method").String() == "GET" && strings.Contains(item.Get("path").String(), "/models") {
					catalogFetch++
				}
				if strings.HasPrefix(item.Get("path").String(), "/v1/alpha/search") {
					alphaCalls++
				}
			}
			savedCatalog, err := os.ReadFile(catalogPath)
			if err != nil || !bytes.Equal(savedCatalog, catalogBody) || catalogFetch != 1 {
				t.Fatal("formal catalog snapshot changed or its authenticated fetch is missing")
			}
			if mode == "alpha" {
				if alphaCalls != 1 || f.posts.Load() != 3 || !strings.Contains(doc.Get("upstream.2.body.input").Raw, "A2_ALPHA_SOURCE") {
					t.Fatalf("Alpha loop incomplete: calls=%d posts=%d", alphaCalls, f.posts.Load())
				}
				if doc.Get("upstream.0.upstream_session").String() != doc.Get("upstream.2.upstream_session").String() || doc.Get("upstream.1.upstream_session").String() == doc.Get("upstream.0.upstream_session").String() {
					t.Fatal("Alpha polluted the reasoning branch")
				}
			} else {
				if childSteps != 2 || childThread == "" {
					t.Fatalf("followup child count=%d thread=%s", childSteps, childThread)
				}
				var session, machine string
				for _, wire := range doc.Get("upstream").Array() {
					if wire.Get("thread_id").String() == childThread {
						if session != "" && (session != wire.Get("upstream_session").String() || machine != wire.Get("machine").String()) {
							t.Fatal("followup child binding moved")
						}
						session, machine = wire.Get("upstream_session").String(), wire.Get("machine").String()
					}
				}
				if !followupSent || !strings.Contains(doc.Get("upstream").Raw, "A2_CHILD_FOLLOWUP") {
					t.Fatal("followup result not returned")
				}
			}
			f.assertReleased(t, int(f.posts.Load()))
			resources := ojson.NewDocString(`{}`)
			_ = resources.Set("observed_at", time.Now().Format(time.RFC3339Nano))
			_ = resources.Set("client_exited", cmd.ProcessState.Exited())
			_ = resources.Set("proxy", f.executor.KiloProxySnapshot())
			_ = resources.Set("credentials", f.manager.CredentialConcurrencySnapshot())
			_ = resources.Set("result_count", len(f.hook.snapshot()))
			_ = resources.Set("upstream_posts", f.posts.Load())
			_ = os.WriteFile(filepath.Join(dir, "resource-snapshot.json"), resources.Bytes(), 0600)
			t.Logf("installed client %s: exact formal catalog snapshot GET=%d; automatic client GET=false; Alpha=%d upstream POST=%d child_turns=%d resources=0", mode, catalogFetch, alphaCalls, f.posts.Load(), childSteps)
		})
	}
}
