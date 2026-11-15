package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSParityModelDiscovery(t *testing.T) {
	for _, scenario := range []string{"discovery", "manual", "disabled", "forbidden", "invalid", "suffix"} {
		t.Run(scenario, func(t *testing.T) {
			enabled := scenario != "disabled"
			cfg := &config.Config{BPS: internalconfig.BPSConfig{AutoDiscover: &enabled}, BPSProtocol: internalconfig.BPSProtocolConfig{StateDir: t.TempDir()}}
			if scenario == "manual" {
				cfg.BPS.Models = []string{"gpt-manual"}
				cfg.BPS.ModelSuffix = "-excel"
			}
			if scenario == "suffix" {
				cfg.BPS.ModelSuffix = "-excel"
			}
			manager := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
			manager.SetConfig(cfg)
			service := &Service{cfg: cfg, coreManager: manager}
			gets, posts := 0, 0
			transport := bpsCatalogTransport(func(r *http.Request) (*http.Response, error) {
				status, body := 200, `{"allowed":true,"model_catalog":{"models":[{"id":"gpt-a"},{"id":"gpt-disabled","available":false},{"id":"gpt-restricted"}],"restricted_models":["gpt-restricted"]}}`
				if r.Method == http.MethodGet {
					gets++
					if r.URL.RawQuery != "include_models=true" {
						t.Fatal(r.URL)
					}
					if r.Header.Get("x-openai-account-id") == "account-b" {
						body = `{"allowed":true,"model_catalog":{"models":[{"id":"gpt-b"}]}}`
					}
					if scenario == "forbidden" {
						status = 403
					}
					if scenario == "invalid" {
						body = `{`
					}
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				posts++
				wire, _ := io.ReadAll(r.Body)
				if ojson.GetBytes(wire, "model").String() != "gpt-a" || r.Header.Get("x-openai-account-id") != "account-a" {
					t.Fatalf("upstream=%s", wire)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_discover\",\"status\":\"completed\",\"output\":[]}}\n\n"))}, nil
			})
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
			for _, account := range []string{"a", "b"} {
				a := &coreauth.Auth{ID: fmt.Sprintf("parity-%s-%s", scenario, account), Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": "account-" + account, "sub": "subject-" + account}}
				if _, err := manager.Register(coreauth.WithSkipPersist(ctx), a); err != nil {
					t.Fatal(err)
				}
				defer GlobalModelRegistry().UnregisterClient(a.ID)
				service.registerExecutorForAuth(a, false)
				service.registerModelsForAuth(ctx, a)
				models := GlobalModelRegistry().GetModelsForClient(a.ID)
				want := "gpt-" + account
				if scenario == "manual" {
					want = "gpt-manual"
				}
				if scenario == "suffix" {
					want += "-excel"
				}
				if scenario == "disabled" || scenario == "forbidden" || scenario == "invalid" {
					if len(models) != 0 {
						t.Fatalf("rejected catalog=%v", models)
					}
				} else if len(models) != 1 || models[0].ID != want {
					t.Fatalf("catalog=%v want=%s", models, want)
				}
			}
			wantGets := 2
			if scenario == "manual" || scenario == "disabled" {
				wantGets = 0
			}
			if gets != wantGets {
				t.Fatalf("gets=%d", gets)
			}
			if scenario == "suffix" {
				_, err := manager.Execute(ctx, []string{"bps"}, coreexecutor.Request{Model: "gpt-a-excel", Payload: []byte(`{"model":"gpt-a-excel","input":"hi"}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
				if err != nil || posts != 1 {
					t.Fatalf("err=%v posts=%d", err, posts)
				}
			}
		})
	}
}

func TestBPSParityModelConfigReloadWithoutPlugins(t *testing.T) {
	for _, home := range []bool{false, true} {
		t.Run(fmt.Sprintf("home=%v", home), func(t *testing.T) {
			cfg := &config.Config{BPS: internalconfig.BPSConfig{Models: []string{"gpt-old"}}, BPSProtocol: internalconfig.BPSProtocolConfig{StateDir: t.TempDir()}}
			cfg.Home.Enabled = home
			manager := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
			manager.SetConfig(cfg)
			service := &Service{cfg: cfg, coreManager: manager}
			auth, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: fmt.Sprintf("bps-reload-%v", home), Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": "reload-account", "sub": "reload-sub"}})
			if err != nil {
				t.Fatal(err)
			}
			defer GlobalModelRegistry().UnregisterClient(auth.ID)
			service.registerModelsForAuth(context.Background(), auth)
			gets := 0
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsCatalogTransport(func(r *http.Request) (*http.Response, error) {
				gets++
				if r.Method != http.MethodGet || r.URL.Host != "bps.openai.com" {
					t.Fatalf("unexpected request %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"allowed":true,"model_catalog":{"models":[{"id":"gpt-new"}]}}`))}, nil
			})))
			next := cfg.CloneForRuntime()
			next.BPS.Models = nil
			next.BPS.ModelSuffix = "-excel"
			if !service.applyConfigUpdateWithAuthSynthesis(ctx, next, false) {
				t.Fatal("reload rejected")
			}
			models := GlobalModelRegistry().GetModelsForClient(auth.ID)
			if len(models) != 1 || models[0].ID != "gpt-new-excel" || gets != 1 {
				t.Fatalf("reload models=%v gets=%d", models, gets)
			}
			off := false
			next = next.CloneForRuntime()
			next.BPS.AutoDiscover = &off
			if !service.applyConfigUpdateWithAuthSynthesis(ctx, next, false) {
				t.Fatal("disable reload rejected")
			}
			if len(GlobalModelRegistry().GetModelsForClient(auth.ID)) != 0 || gets != 1 {
				t.Fatal("disabled discovery retained stale models")
			}
		})
	}
}

func TestBPSParityStaleCatalogIsDiscarded(t *testing.T) {
	for _, change := range []string{"account", "suffix"} {
		t.Run(change, func(t *testing.T) {
			cfg := &config.Config{}
			manager := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
			manager.SetConfig(cfg)
			service := &Service{cfg: cfg, coreManager: manager}
			auth, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: "bps-stale-" + change, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": "old-account", "sub": "old-sub"}})
			if err != nil {
				t.Fatal(err)
			}
			defer GlobalModelRegistry().UnregisterClient(auth.ID)
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsCatalogTransport(func(*http.Request) (*http.Response, error) {
				if change == "account" {
					next := auth.Clone()
					next.Metadata["account_id"] = "new-account"
					if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), next); err != nil {
						t.Fatal(err)
					}
				} else {
					next := cfg.CloneForRuntime()
					next.BPS.ModelSuffix = "-excel"
					service.commitConfigUpdate(next)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"allowed":true,"model_catalog":{"models":[{"id":"gpt-stale"}]}}`))}, nil
			})))
			if service.registerModelsForAuth(ctx, auth) || len(GlobalModelRegistry().GetModelsForClient(auth.ID)) != 0 {
				t.Fatal("stale discovery was published")
			}
		})
	}
}
