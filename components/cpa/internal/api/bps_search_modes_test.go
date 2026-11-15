package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type inlineSearchCallerSelector struct {
	t                *testing.T
	searchSelections int
}

func (s *inlineSearchCallerSelector) Pick(ctx context.Context, provider, _ string, opts coreexecutor.Options, candidates []*coreauth.Auth) (*coreauth.Auth, error) {
	if provider == "codex" {
		s.searchSelections++
		if scope, _ := opts.Metadata[coreexecutor.CallerScopeMetadataKey].(string); scope == "" || scope == "search-key" {
			s.t.Errorf("nested caller scope missing or raw: %q", scope)
		}
		if ctx.Value("gin") != nil || opts.Metadata[coreexecutor.PinnedAuthMetadataKey] != nil || opts.Metadata[coreexecutor.ExecutionSessionMetadataKey] != nil {
			s.t.Error("nested selection retained outer execution identity")
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return candidates[0], nil
}

func TestBPSInlineSearchHTTPModes(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		for _, stream := range []bool{false, true} {
			for _, failed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%v/failure=%v", path, stream, failed), func(t *testing.T) {
					server, cfg, _, live := newAlphaFixture(t, "bps")
					manager := server.handlers.AuthManager
					manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
					codex := registerAlphaCodex(t, server, "alpha-codex-search")
					if failed {
						codex.statuses = []int{403}
					}
					selector := &inlineSearchCallerSelector{t: t}
					manager.SetSelector(selector)
					posts := 0
					manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(req *http.Request) (*http.Response, error) {
						posts++
						body, _ := io.ReadAll(req.Body)
						if posts == 2 && (!strings.Contains(string(body), "Native Alpha source") || !strings.Contains(string(body), "function_call_output")) {
							t.Fatalf("missing text-only continuation: %s", body)
						}
						output := []any{bpsSearchNativeCall("search-one", "__cpa_bps_web_search")}
						if posts > 1 {
							output = []any{map[string]any{"type": "message", "id": "answer", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "MODE_RESULT", "annotations": []any{}}}}}
						}
						terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_inline","status":"completed","model":"alpha-bps-model"}}`)
						_ = terminal.Set("response.output", output)
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
					}))
					body := ojson.NewDocString(`{"model":"alpha-bps-model","input":"search","tools":[{"type":"web_search","external_web_access":false,"filters":{"allowed_domains":["example.com"]}}]}`)
					mode := any(false)
					if strings.Contains(path, "chat") {
						_ = body.Delete("input")
						_ = body.Set("messages", []any{map[string]any{"role": "user", "content": "search"}})
						mode = "indexed"
						_ = body.Set("tools.0.external_web_access", mode)
					}
					_ = body.Set("stream", stream)
					req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body.String()))
					req.Header.Set("Authorization", "Bearer search-key")
					req.Header.Set("User-Agent", "codex_cli_rs/0.169.0-alpha.11.1")
					response := httptest.NewRecorder()
					server.engine.ServeHTTP(response, req)
					if codex.httpCalls != 1 || selector.searchSelections != 1 || live.calls != 0 {
						t.Fatalf("search dispatch: alpha=%d selection=%d live=%d body=%s", codex.httpCalls, selector.searchSelections, live.calls, response.Body.String())
					}
					if got := ojson.GetBytes(codex.body, "settings.external_web_access").Value(); got != mode {
						t.Fatalf("mode=%v want=%v", got, mode)
					}
					if ojson.GetBytes(codex.body, "settings.filters.allowed_domains.0").String() != "example.com" || ojson.GetBytes(codex.body, "commands.search_query.0.q").String() != "synthetic query" {
						t.Fatalf("constraints lost: %s", codex.body)
					}
					if failed {
						if posts != 1 || strings.Contains(response.Body.String(), "MODE_RESULT") || strings.Contains(response.Body.String(), `"type":"response.completed"`) {
							t.Fatalf("failed search replayed: posts=%d body=%s", posts, response.Body.String())
						}
						if !stream && response.Code < 400 {
							t.Fatalf("failed search succeeded: %d %s", response.Code, response.Body.String())
						}
						if stream && path == "/v1/responses" {
							assertBPSFailedResponseIdentity(t, response.Body.String(), "resp_bps_inline")
						}
					} else if posts != 2 || response.Code != 200 || !strings.Contains(response.Body.String(), "MODE_RESULT") || strings.Contains(response.Body.String(), `"sources":[]`) {
						t.Fatalf("success contract: status=%d posts=%d body=%s", response.Code, posts, response.Body.String())
					}
					parent, _ := manager.GetByID("alpha-bps-auth")
					if len(parent.PermissionSuspensions) != 0 {
						t.Fatal("search denial suspended parent BPS")
					}
				})
			}
		}
	}
}

func assertBPSFailedResponseIdentity(t *testing.T, stream, expectedID string) {
	t.Helper()
	created, failed, completed := 0, 0, 0
	createdID := ""
	for _, line := range strings.Split(stream, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		switch ojson.Get(payload, "type").String() {
		case "response.created":
			created++
			createdID = ojson.Get(payload, "response.id").String()
			if createdID != expectedID {
				t.Fatalf("created ID=%q want=%q: %s", createdID, expectedID, stream)
			}
		case "response.failed":
			failed++
			if created != 1 || ojson.Get(payload, "response.id").String() != createdID || ojson.Get(payload, "response_id").String() != createdID {
				t.Fatalf("failed identity differs from delivered created: %s", stream)
			}
		case "response.completed":
			completed++
		}
	}
	if created != 1 || failed != 1 || completed != 0 {
		t.Fatalf("terminal counts: created=%d failed=%d completed=%d stream=%s", created, failed, completed, stream)
	}
}

func TestBPSInlineSearchHTTPFailureResponseID(t *testing.T) {
	for _, scenario := range []string{"backend403", "missing_config", "missing_credential", "invalid_response", "continuation403", "continuation_transport"} {
		t.Run(scenario, func(t *testing.T) {
			server, cfg, _, _ := newAlphaFixture(t, "bps")
			manager := server.handlers.AuthManager
			manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
			codex := registerAlphaCodex(t, server, "alpha-codex-search")
			wantSearchCalls, wantPosts := 1, 1
			if strings.HasPrefix(scenario, "continuation") {
				wantPosts = 2
			}
			switch scenario {
			case "backend403":
				codex.statuses = []int{403}
			case "missing_config":
				cfg.Plugins.Configs["claude-web-search-router"] = alphaPluginConfig(t, "enabled: true")
				wantSearchCalls = 0
			case "missing_credential":
				selected, _ := manager.GetByID("alpha-codex-backend")
				selected.Disabled = true
				if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), selected); err != nil {
					t.Fatal(err)
				}
				wantSearchCalls = 0
			case "invalid_response":
				codex.responseBody = io.NopCloser(strings.NewReader(`{"output":null}`))
			}
			posts := 0
			manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(*http.Request) (*http.Response, error) {
				posts++
				if posts == 2 {
					if scenario == "continuation_transport" {
						return nil, errors.New("synthetic continuation transport failure")
					}
					return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("synthetic continuation denied"))}, nil
				}
				terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_failure_parent","status":"completed","model":"alpha-bps-model"}}`)
				_ = terminal.Set("response.output", []any{bpsSearchNativeCall("failed-search", "__cpa_bps_web_search")})
				created := `data: {"type":"response.created","response":{"id":"resp_failure_parent","status":"in_progress","output":[]}}` + "\n\n"
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(created + "data: " + terminal.String() + "\n\n"))}, nil
			}))
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"id":"client-request-is-not-response-id","model":"alpha-bps-model","stream":true,"input":"search","tools":[{"type":"web_search","external_web_access":false}]}`))
			req.Header.Set("Authorization", "Bearer search-key")
			req.Header.Set("User-Agent", "codex_cli_rs/0.169.0-alpha.11.1")
			response := httptest.NewRecorder()
			server.engine.ServeHTTP(response, req)
			if response.Code != 200 || posts != wantPosts || codex.httpCalls != wantSearchCalls {
				t.Fatalf("status=%d BPS posts=%d search calls=%d want=%d body=%s", response.Code, posts, codex.httpCalls, wantSearchCalls, response.Body.String())
			}
			assertBPSFailedResponseIdentity(t, response.Body.String(), "resp_bps_failure_parent")
			parent, _ := manager.GetByID("alpha-bps-auth")
			if scenario != "continuation403" && len(parent.PermissionSuspensions) != 0 {
				t.Fatal("search failure suspended parent BPS")
			}
			if scenario == "continuation403" && len(parent.PermissionSuspensions) == 0 {
				t.Fatal("real BPS continuation denial lost permission attribution")
			}
		})
	}
}
