package openai

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

func TestBPSDoesNotBreakCodexWebsocketProviderSet(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	const model = "bps-ws-shared-test"
	reg.RegisterClient("bps-ws-test", "bps", []*registry.ModelInfo{{ID: model}})
	reg.RegisterClient("codex-ws-test", "codex", []*registry.ModelInfo{{ID: model}})
	defer reg.UnregisterClient("bps-ws-test")
	defer reg.UnregisterClient("codex-ws-test")
	providers, _ := responsesWebsocketProviderSetForModel(model)
	if _, found := providers["bps"]; found {
		t.Fatal("BPS entered WebSocket provider set")
	}
	if _, found := providers["codex"]; !found || len(providers) != 1 {
		t.Fatalf("Codex passthrough set changed: %v", providers)
	}
}
