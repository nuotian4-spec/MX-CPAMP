package cliproxy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSModelRegistrationIsExplicitAndDeduplicated(t *testing.T) {
	ctx := context.Background()
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	service := &Service{cfg: &config.Config{BPS: internalconfig.BPSConfig{Models: []string{"shared-model", "shared-model", ""}}}, coreManager: manager}
	auth := &coreauth.Auth{ID: "bps-model-catalog-test", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"type": "bps"}}
	if _, err := manager.Register(ctx, auth); err != nil {
		t.Fatal(err)
	}
	defer GlobalModelRegistry().UnregisterClient(auth.ID)
	service.registerExecutorForAuth(auth, false)
	if executor, ok := manager.Executor("bps"); !ok || executor.Identifier() != "bps" {
		t.Fatal("BPS executor not bound")
	}
	if !service.registerModelsForAuth(ctx, auth) {
		t.Fatal("model registration fenced")
	}
	models := GlobalModelRegistry().GetModelsForClient(auth.ID)
	if len(models) != 1 || models[0].ID != "shared-model" {
		t.Fatalf("catalog=%v", models)
	}
	service.cfg = &config.Config{}
	service.registerModelsForAuth(ctx, auth)
	if len(GlobalModelRegistry().GetModelsForClient(auth.ID)) != 0 {
		t.Fatal("unconfigured BPS models remained published")
	}
}

type bpsCatalogTransport func(*http.Request) (*http.Response, error)

func (f bpsCatalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBPSRegisteredModelsApplyReasoningSuffix(t *testing.T) {
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	cfg := &config.Config{BPS: internalconfig.BPSConfig{Models: []string{"bps-thinking-model"}}}
	manager.SetConfig(cfg)
	service := &Service{cfg: cfg, coreManager: manager}
	auth := &coreauth.Auth{ID: "bps-thinking-catalog", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"type": "bps", "account_id": "fake-account", "access_token": "fake-access", "expired": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatal(err)
	}
	defer GlobalModelRegistry().UnregisterClient(auth.ID)
	service.registerExecutorForAuth(auth, false)
	service.registerModelsForAuth(context.Background(), auth)
	transport := bpsCatalogTransport(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if ojson.GetBytes(body, "reasoning_effort").String() != "high" {
			t.Fatalf("registered catalog lost suffix intent: %s", body)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_catalog\",\"status\":\"completed\",\"output\":[]}}\n\n"))}, nil
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
	_, err := manager.Execute(ctx, []string{"bps"}, coreexecutor.Request{Model: "bps-thinking-model(high)", Payload: []byte(`{"input":"hi","reasoning":{"effort":"low"}}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err != nil {
		t.Fatal(err)
	}
}
