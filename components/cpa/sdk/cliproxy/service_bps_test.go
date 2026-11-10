package cliproxy

import (
	"context"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
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
