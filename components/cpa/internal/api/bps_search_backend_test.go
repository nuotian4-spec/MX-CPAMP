package api

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type bpsSearchContractProbe struct {
	*executor.BPSExecutor
	run func(context.Context)
}

func (e *bpsSearchContractProbe) Execute(ctx context.Context, _ *coreauth.Auth, _ coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.run(ctx)
	return coreexecutor.Response{Payload: []byte(`{"id":"probe","object":"response","status":"completed","output":[]}`)}, nil
}

func TestBPSSearchTypedBackendContract(t *testing.T) {
	for _, scenario := range []string{"cached", "indexed", "live", "live_empty", "no_credential", "live_no_credential", "backend_unavailable", "http_rejected", "invalid_response", "mode_constraint", "domain_constraint", "sources_required", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			server, cfg, _, live := newAlphaFixture(t, "bps")
			codex := registerAlphaCodex(t, server, "alpha-codex-search")
			request := coreexecutor.BPSSearchRequest{Mode: coreexecutor.BPSSearchCached, Query: "synthetic query", CallerScope: "typed-caller", SessionID: "typed-session", AllowedDomains: []string{"example.com"}}
			wantKind, wantCalls := "", 1
			switch scenario {
			case "indexed":
				request.Mode = coreexecutor.BPSSearchIndexed
			case "live", "live_empty", "live_no_credential":
				request.Mode = coreexecutor.BPSSearchLive
				wantCalls = 0
				if scenario == "live_empty" {
					live.body = `{"content":[{"type":"web_search_tool_result","content":[]}]}`
				}
				if scenario == "live_no_credential" {
					a, _ := server.handlers.AuthManager.GetByID("alpha-search-backend")
					a.Disabled = true
					if _, err := server.handlers.AuthManager.Update(coreauth.WithSkipPersist(context.Background()), a); err != nil {
						t.Fatal(err)
					}
					wantKind = coreexecutor.BPSSearchNoCredential
				}
			case "no_credential":
				a, _ := server.handlers.AuthManager.GetByID("alpha-codex-backend")
				a.Disabled = true
				if _, err := server.handlers.AuthManager.Update(coreauth.WithSkipPersist(context.Background()), a); err != nil {
					t.Fatal(err)
				}
				wantKind, wantCalls = coreexecutor.BPSSearchNoCredential, 0
			case "backend_unavailable":
				cfg.Plugins.Configs["claude-web-search-router"] = alphaPluginConfig(t, "enabled: true")
				wantKind, wantCalls = coreexecutor.BPSSearchBackendUnavailable, 0
			case "http_rejected":
				codex.statuses = []int{403}
				wantKind = coreexecutor.BPSSearchHTTPRejected
			case "invalid_response":
				codex.responseBody = io.NopCloser(strings.NewReader(`{"output":null,"credential":"SECRET"}`))
				wantKind = coreexecutor.BPSSearchInvalidResponse
			case "mode_constraint", "domain_constraint":
				params := map[string]any{"settings.external_web_access": true}
				if scenario == "domain_constraint" {
					params = map[string]any{"settings.filters.allowed_domains": []string{"other.invalid"}}
				}
				cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "alpha-codex-search"}}, Params: params}}
				wantKind, wantCalls = coreexecutor.BPSSearchPolicyRejected, 0
			case "sources_required":
				request.RequireSources = true
				wantKind, wantCalls = coreexecutor.BPSSearchPolicyRejected, 0
			case "canceled":
				wantCalls = 0
			}
			called := false
			probe := &bpsSearchContractProbe{BPSExecutor: executor.NewBPSExecutor(cfg), run: func(ctx context.Context) {
				called = true
				backend, ok := coreexecutor.BPSWebSearchBackendFromContext(ctx)
				if !ok {
					t.Fatal("typed backend missing")
				}
				preflightErr := backend.Preflight(ctx, request)
				if codex.httpCalls != 0 || live.calls != 0 {
					t.Fatal("preflight dispatched search")
				}
				if scenario != "backend_unavailable" && scenario != "sources_required" && preflightErr != nil {
					t.Fatalf("unexpected preflight failure: %v", preflightErr)
				}
				if scenario == "canceled" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				result, err := backend.Execute(ctx, request)
				if scenario == "canceled" {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel cause lost: %v", err)
					}
					return
				}
				if wantKind != "" {
					var typed *coreexecutor.BPSSearchError
					if !errors.As(err, &typed) || typed.Kind != wantKind {
						t.Fatalf("error=%v want=%s", err, wantKind)
					}
					if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "synthetic") {
						t.Fatalf("backend details leaked: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if request.Mode != coreexecutor.BPSSearchLive {
					if result.Text != "Native Alpha source https://example.com" || result.SourcesProvided || len(result.Sources) != 0 {
						t.Fatalf("text-only result=%+v", result)
					}
				} else {
					if !result.SourcesProvided {
						t.Fatalf("live source evidence lost: %+v", result)
					}
					if scenario == "live" && (result.Text != "Actual backend findings." || len(result.Sources) != 1) {
						t.Fatalf("live text/result=%+v", result)
					}
					if scenario == "live_empty" && len(result.Sources) != 0 {
						t.Fatalf("empty result invented sources: %+v", result)
					}
				}
			}}
			server.handlers.AuthManager.RegisterExecutor(probe)
			_, failure := server.handlers.ExecuteModel(context.Background(), handlers.ModelExecutionRequest{EntryProtocol: "openai-response", ExitProtocol: "openai-response", Model: "alpha-bps-model", Body: []byte(`{"model":"alpha-bps-model","input":"probe"}`), CallerScope: "typed-caller"})
			if failure != nil || !called || codex.httpCalls != wantCalls {
				t.Fatalf("probe=%v failure=%v calls=%d want=%d", called, failure, codex.httpCalls, wantCalls)
			}
		})
	}
}

func TestBPSAlphaSearchPerQueryDomainBarrier(t *testing.T) {
	server, cfg, _, _ := newAlphaFixture(t, "bps")
	codex := registerAlphaCodex(t, server, "alpha-codex-search")
	cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "alpha-codex-search"}}, Params: map[string]any{"commands.search_query.0.domains": []string{"other.invalid"}}}}
	body := alphaBody()
	_ = body.Set("settings.external_web_access", "indexed")
	response := alphaRequest(server, context.Background(), "/v1/alpha/search", body.Bytes(), "search-key")
	if response.Code != 502 || codex.httpCalls != 0 {
		t.Fatalf("per-query domains overwritten: status=%d calls=%d body=%s", response.Code, codex.httpCalls, response.Body.String())
	}
}
