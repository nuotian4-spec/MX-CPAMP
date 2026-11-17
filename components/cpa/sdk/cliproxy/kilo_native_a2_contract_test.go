package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
)

func TestKiloSearchA2NativeContractScenarios(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		status, posts, results, completed int
		code                              string
	}{
		{"live_domains", 200, 1, 1, 0, ""}, {"empty", 200, 1, 1, 0, ""},
		{"no_search", 502, 1, 1, 0, "upstream_response_invalid"}, {"no_sources", 502, 1, 1, 0, "upstream_response_invalid"},
		{"failed_terminal", 502, 1, 1, 0, "request_failed"}, {"middle_failure", 502, 2, 2, 1, "upstream_response_invalid"},
		{"final_filter", 400, 0, 1, 0, "invalid_request"}, {"late_filter", 400, 1, 2, 1, "invalid_request"},
		{"mode_conflict", 400, 0, 1, 0, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := 0
			var f *kiloNativeFixture
			filter := func() {
				next := *f.cfg
				next.Payload.Filter = []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: []string{"tools.0.filters"}}}
				f.executor.UpdateConfig(&next)
				f.manager.SetConfig(&next)
			}
			f = newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				step++
				raw, _ := io.ReadAll(r.Body)
				if tc.name == "live_domains" && (!ojson.GetBytes(raw, "tools.0.external_web_access").Bool() || ojson.GetBytes(raw, "tools.0.filters.allowed_domains.0").String() != "docs.example.com") {
					t.Error("live or domain intersection lost")
				}
				if tc.name == "no_search" {
					kiloSuccessStream(w, r)
					return
				}
				if tc.name == "failed_terminal" {
					w.Header().Set("Content-Type", "text/event-stream")
					kiloA1Event(w, `{"type":"response.failed","response":{"id":"resp-failed","status":"failed","error":{"code":"synthetic_failure"}}}`)
					return
				}
				action := map[string]any{"type": "search", "sources": []any{map[string]any{"url": "https://docs.example.com/source", "title": "Synthetic"}}}
				if tc.name == "no_sources" || (tc.name == "middle_failure" && step == 2) {
					delete(action, "sources")
				}
				items := []any{map[string]any{"type": "web_search_call", "id": fmt.Sprintf("ws-%d", step), "status": "completed", "action": action}}
				if tc.name == "empty" {
					action["sources"] = []any{}
				} else {
					items = append(items, kiloA1Message(fmt.Sprintf("msg-%d", step), "synthetic search"))
				}
				if tc.name == "late_filter" && step == 1 {
					filter()
				}
				kiloA1ClientOutput(w, fmt.Sprintf("resp-contract-%d", step), items)
			})
			if tc.name == "final_filter" {
				filter()
			}
			if tc.name == "mode_conflict" {
				next := *f.cfg
				next.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"tools.0.external_web_access": true}}}
				f.executor.UpdateConfig(&next)
				f.manager.SetConfig(&next)
			}
			doc := ojson.NewDocString(`{"id":"synthetic-parent","model":"gpt-6.1-sol","input":[],"commands":{"search_query":[{"q":"first","domains":["docs.example.com"]}]},"settings":{"filters":{"allowed_domains":["example.com"],"blocked_domains":["other.invalid"]}}}`)
			if tc.name == "live_domains" {
				_ = doc.Set("settings.external_web_access", true)
			}
			if tc.name == "middle_failure" || tc.name == "late_filter" {
				_ = doc.SetRaw("commands.search_query", `[{"q":"first","domains":["docs.example.com"]},{"q":"second","domains":["docs.example.com"]},{"q":"third","domains":["docs.example.com"]}]`)
			}
			before := helps.PayloadConfigEvaluationCount()
			status, data := f.request(t, "POST", "/v1/alpha/search", "synthetic-kilo-key", doc.String())
			if status != tc.status || (tc.code != "" && ojson.GetBytes(data, "error.code").String() != tc.code) {
				t.Fatalf("result=%d %s", status, data)
			}
			if tc.status != 200 && ojson.GetBytes(data, "error.completed_queries").Int() != int64(tc.completed) {
				t.Fatalf("completed count=%s", data)
			}
			if tc.status != 200 {
				assertKiloPublicErrorClean(t, data)
				if ojson.GetBytes(data, "error.request_id").String() == "" {
					t.Fatalf("missing error correlation: %s", data)
				}
			}
			if f.posts.Load() != int32(tc.posts) || int(helps.PayloadConfigEvaluationCount()-before) != tc.results {
				t.Fatalf("POST=%d payload evaluations=%d", f.posts.Load(), helps.PayloadConfigEvaluationCount()-before)
			}
			f.assertReleased(t, tc.results)
			results := f.hook.snapshot()
			if results[len(results)-1].Success != (tc.status == 200) {
				t.Fatalf("business result misclassified: %+v", results)
			}
			if tc.completed > 0 && !results[0].Success {
				t.Fatal("prior success was rolled back")
			}
			t.Logf("status=%d POST=%d results=%d completed=%d resources=0 no_replay=true", status, tc.posts, tc.results, tc.completed)
		})
	}
}

func TestKiloSearchA2ScopedCatalogAndAdmission(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream)
	for _, key := range []string{"synthetic-legacy-key", "synthetic-codex-key", "synthetic-bps-key"} {
		status, body := f.request(t, "POST", "/v1/alpha/search", key, `{"id":"synthetic","model":"gpt-6.1-sol","commands":{"search_query":[{"q":"query"}]}}`)
		if status != 403 {
			t.Fatalf("key=%s status=%d %s", key, status, body)
		}
	}
	status, _ := f.request(t, "GET", "/v1/models?client_version=0.169.0-alpha.11.1", "wrong-key", "")
	if status != 401 {
		t.Fatalf("unauthenticated catalog=%d", status)
	}
	registry.GetGlobalRegistry().RegisterClient("a2-other-provider", "codex", []*registry.ModelInfo{{ID: "gpt-6.1-sol"}, {ID: "a2-codex-only"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient("a2-other-provider") })
	for _, key := range []string{"synthetic-kilo-key", "synthetic-legacy-key", "synthetic-codex-key", "synthetic-bps-key"} {
		status, raw := f.request(t, "GET", "/v1/models?client_version=0.169.0-alpha.11.1", key, "")
		if status != 200 {
			t.Fatalf("catalog=%d %s", status, raw)
		}
		for _, model := range ojson.GetBytes(raw, "models").Array() {
			if key == "synthetic-kilo-key" && model.Get("slug").String() == "a2-codex-only" {
				t.Fatal("cross-channel model published")
			}
			if model.Get("slug").String() == "gpt-6.1-sol" && model.Get("supports_search_tool").Bool() != (key == "synthetic-kilo-key") {
				t.Fatalf("same-name capability escaped scope: key=%s model=%s", key, model.Raw)
			}
		}
	}
	for _, body := range []string{
		`{"model":"gpt-6.1-sol","client_metadata":{"thread_id":"ordinary","kilo_internal_search":true},"input":[{"role":"assistant","content":"history"}]}`,
		`{"model":"gpt-6.1-sol","client_metadata":{"thread_id":"ordinary"},"input":[{"type":"function_call_output","call_id":"unowned","output":"x"}]}`,
	} {
		req, _ := http.NewRequest("POST", f.server.URL+"/v1/responses", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Kilo-Internal-Search", "true")
		response, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 409 {
			t.Fatalf("external marker/history=%d %s", response.StatusCode, data)
		}
	}
	if f.posts.Load() != 0 {
		t.Fatal("rejected scope or forged internal marker went upstream")
	}
	for _, a := range f.manager.List() {
		a.Disabled = true
		if _, err := f.manager.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	status, raw := f.request(t, "GET", "/v1/models?client_version=0.169.0-alpha.11.1", "synthetic-kilo-key", "")
	if status != 200 || len(ojson.GetBytes(raw, "models").Array()) != 0 {
		t.Fatalf("disabled executor credentials advertised: %d %s", status, raw)
	}
}
