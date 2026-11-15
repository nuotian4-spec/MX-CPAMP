package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type combinedNativeCodexHTTPTransport struct {
	*executor.CodexAutoExecutor
	transport http.RoundTripper
}

func (e *combinedNativeCodexHTTPTransport) HttpRequest(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	if e.transport == nil || auth.ProxyURL != "" || coreexecutor.RequestProxyURL(ctx) != "" {
		return nil, errors.New("combined fixture rejects proxy overrides or missing HTTP transport")
	}
	// Nested search isolates the outer transport; inject only at the native HTTP boundary.
	ctx = context.WithValue(ctx, "cliproxy.roundtripper", e.transport)
	return e.CodexAutoExecutor.HttpRequest(ctx, auth, req.WithContext(ctx))
}

func TestCombinedBPSNativeAlphaAndTypedSchemaRecovery(t *testing.T) {
	denyTransport := bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unmatched HTTP transport rejected: method=%s host=%s path=%s", r.Method, r.URL.Host, r.URL.Path)
		return nil, errors.New("combined fixture rejects unmatched HTTP transport")
	})
	previousTransport, previousClient := http.DefaultTransport, http.DefaultClient
	http.DefaultTransport = denyTransport
	http.DefaultClient = &http.Client{Transport: denyTransport}
	t.Cleanup(func() {
		http.DefaultTransport, http.DefaultClient = previousTransport, previousClient
	})
	// Only this test process loses inherited proxy settings; production config is unchanged.
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	newServer := func(t *testing.T) (*Server, *coreauth.Manager, *combinedNativeCodexHTTPTransport) {
		t.Helper()
		cfg := &config.Config{
			SDKConfig: sdkconfig.SDKConfig{
				APIKeys: []string{"combined-legacy-key", "combined-bps-key", "combined-codex-key"},
				ClientKeyScopes: []sdkconfig.ClientKeyScope{
					{APIKey: "combined-bps-key", Channel: "bps"},
					{APIKey: "combined-codex-key", Channel: "codex"},
				},
			},
			BPS:         config.BPSConfig{Models: []string{"combined-bps-model"}},
			BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()},
			Plugins: config.PluginsConfig{Enabled: true, Configs: map[string]config.PluginInstanceConfig{
				"claude-web-search-router": alphaPluginConfig(t, "enabled: true\ncodex_model: combined-codex-search"),
			}},
		}
		s := newTestServerWithConfig(t, cfg)
		m := s.handlers.AuthManager
		m.SetConfig(cfg)
		m.SetRoundTripperProvider(denyTransport)
		m.SetSelector(&coreauth.FillFirstSelector{})
		m.RegisterExecutor(executor.NewBPSExecutor(cfg))
		codex := &combinedNativeCodexHTTPTransport{CodexAutoExecutor: executor.NewCodexAutoExecutor(cfg), transport: denyTransport}
		m.RegisterExecutor(codex)
		m.RegisterExecutor(executor.NewKiloExecutor(cfg))
		return s, m, codex
	}
	register := func(t *testing.T, m *coreauth.Manager, id, provider, model string) {
		t.Helper()
		a := &coreauth.Auth{ID: id, Provider: provider, Status: coreauth.StatusActive, Metadata: map[string]any{
			"access_token": "synthetic-" + id, "account_id": id, "sub": id,
		}}
		if _, err := m.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, provider, []*registry.ModelInfo{{ID: model, Type: provider, UserDefined: true}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	request := func(s *Server, transport http.RoundTripper, path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), "cliproxy.roundtripper", transport))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, r)
		return w
	}
	assertReleased := func(t *testing.T, m *coreauth.Manager) {
		t.Helper()
		snapshot := m.CredentialConcurrencySnapshot()
		if snapshot["pool_waiting"] != 0 {
			t.Fatalf("pool waiters retained: %+v", snapshot)
		}
		for _, entry := range snapshot["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			if entry.Active != 0 || entry.PinnedWaiting != 0 {
				t.Fatalf("credential lease retained: %+v", entry)
			}
		}
	}

	t.Run("alpha_aliases", func(t *testing.T) {
		s, m, codex := newServer(t)
		register(t, m, "combined-alpha-bps", "bps", "combined-bps-model")
		register(t, m, "combined-alpha-codex", "codex", "combined-codex-search")
		posts := 0
		transport := bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost || r.URL.String() != "https://chatgpt.com/backend-api/codex/alpha/search" {
				return denyTransport.RoundTrip(r)
			}
			posts++
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if r.Header.Get("Authorization") != "Bearer synthetic-combined-alpha-codex" || ojson.GetBytes(raw, "model").String() != "combined-codex-search" || ojson.GetBytes(raw, "settings.external_web_access").String() != "cached" {
				t.Fatalf("native Alpha route changed: url=%s body=%s", r.URL, raw)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"output":"Synthetic native Alpha source","encrypted_output":null}`))}, nil
		})
		m.SetRoundTripperProvider(transport)
		codex.transport = transport
		body := `{"id":"combined-alpha","model":"combined-bps-model","commands":{"search_query":[{"q":"synthetic"}]},"settings":{"external_web_access":"cached"}}`
		for _, path := range []string{"/v1/alpha/search", "/backend-api/codex/alpha/search"} {
			before := posts
			w := request(s, transport, path, "combined-legacy-key", body)
			if w.Code != http.StatusOK || posts != before+1 || ojson.GetBytes(w.Body.Bytes(), "output").String() != "Synthetic native Alpha source" {
				t.Fatalf("legacy Alpha alias failed: path=%s status=%d posts=%d body=%s", path, w.Code, posts, w.Body.String())
			}
			for _, key := range []string{"combined-bps-key", "combined-codex-key"} {
				w = request(s, transport, path, key, body)
				if w.Code != http.StatusForbidden || posts != before+1 {
					t.Fatalf("channel isolation failed: key=%s status=%d posts=%d body=%s", key, w.Code, posts, w.Body.String())
				}
			}
			assertReleased(t, m)
		}
		t.Logf("native_alpha_aliases=2 upstream_posts=%d scoped_cross_channel_posts=0 leases=0", posts)
	})

	for _, tc := range []struct {
		name, invalid, reason string
		accounts, posts       int
		success, stream       bool
	}{
		{"schema_recovery", `{"n":"not-a-number"}`, "schema_mismatch", 2, 2, true, false},
		{"schema_recovery_stream", `{"n":"not-a-number"}`, "schema_mismatch", 2, 2, true, true},
		{"resource_rejection", `{"n":1e4097}`, "number_exponent_limit", 2, 1, false, false},
		{"no_same_channel_candidate", `{"n":"not-a-number"}`, "schema_mismatch", 1, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, m, _ := newServer(t)
			for i := 0; i < tc.accounts; i++ {
				register(t, m, fmt.Sprintf("combined-%c", 'A'+i), "bps", "combined-bps-model")
			}
			register(t, m, "combined-codex-other", "codex", "combined-bps-model")
			register(t, m, "combined-kilo-other", "kilo", "combined-bps-model")
			var posted []string
			transport := failoverHeaderTransport{
				selected: func(a *coreauth.Auth) {
					if a.Provider != "bps" {
						t.Fatalf("typed recovery crossed provider: %s", a.Provider)
					}
				},
				bpsDiagnosticTransport: func(r *http.Request) (*http.Response, error) {
					if r.Method != http.MethodPost || r.URL.String() != "https://bps.openai.com/basispoints/api/responses" {
						return denyTransport.RoundTrip(r)
					}
					id := r.Header.Get("chatgpt-account-id")
					posted = append(posted, id)
					arguments := tc.invalid
					if id == "combined-B" {
						arguments = `{"n":9007199254740993}`
					}
					envelope := ojson.NewDocString(`{"name":"echo"}`)
					_ = envelope.SetRaw("arguments", arguments)
					outer := ojson.NewDocString(`{"summary":"Call client tool","extended_summary":"","destructive":false,"references":[]}`)
					_ = outer.Set("code", envelope.String())
					terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_combined","status":"completed","model":"combined-bps-model"}}`)
					_ = terminal.Set("response.output", []any{map[string]any{"type": "function_call", "id": "fc_combined", "call_id": "call_combined", "name": "run_officejs", "arguments": outer.String(), "status": "completed"}})
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
				},
			}
			m.SetRoundTripperProvider(transport)
			body := `{"model":"combined-bps-model","input":"Call echo","tools":[{"type":"function","name":"echo","parameters":{"type":"object","properties":{"n":{"type":"number"}},"required":["n"],"additionalProperties":false}}]}`
			doc := ojson.NewDocString(body)
			_ = doc.Set("stream", tc.stream)
			w := request(s, transport, "/v1/responses", "combined-bps-key", doc.String())
			wantPosts := []string{"combined-A"}
			if tc.posts == 2 {
				wantPosts = append(wantPosts, "combined-B")
			}
			if !reflect.DeepEqual(posted, wantPosts) || (w.Code == http.StatusOK) != tc.success {
				t.Fatalf("typed recovery failed: status=%d posts=%v want=%v body=%s", w.Code, posted, wantPosts, w.Body.String())
			}
			if tc.success {
				call := ojson.GetBytes(w.Body.Bytes(), "output.0")
				if tc.stream {
					completed := 0
					for _, line := range strings.Split(w.Body.String(), "\n") {
						event := ojson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
						if event.Get("type").String() == "response.completed" {
							completed++
							call = event.Get("response.output.0")
						}
					}
					if completed != 1 {
						t.Fatalf("stream completion count=%d", completed)
					}
				}
				if !bpsbinding.IsCallID(call.Get("call_id").String()) || call.Get("name").String() != "echo" || call.Get("arguments").String() != `{"n":9007199254740993}` || w.Header().Get("X-BPS-Failover-Count") != "1" {
					t.Fatalf("recovered call contract changed: %s", w.Body.String())
				}
			} else if ojson.GetBytes(w.Body.Bytes(), "error.diagnostics.validation_reason").String() != tc.reason || strings.Contains(w.Body.String(), "call_bps_") {
				t.Fatalf("typed diagnostic or atomic withholding changed: %s", w.Body.String())
			}
			assertReleased(t, m)
			t.Logf("native_bps status=%d posts=%v codex_posts=0 kilo_posts=0 leases=0", w.Code, posted)
		})
	}
}
