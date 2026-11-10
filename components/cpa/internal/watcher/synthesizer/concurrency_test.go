package synthesizer

import (
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"testing"
	"time"
)

func TestCredentialConcurrencyConfigIdentityStable(t *testing.T) {
	limit := 7
	cfg := &config.Config{GeminiKey: []config.GeminiKey{{APIKey: "fake-gemini"}}, InteractionsKey: []config.GeminiKey{{APIKey: "fake-interactions"}}, ClaudeKey: []config.ClaudeKey{{APIKey: "fake-claude"}}, CodexKey: []config.CodexKey{{APIKey: "fake-codex"}}, XAIKey: []config.CodexKey{{APIKey: "fake-xai"}}, MetaKey: []config.CodexKey{{APIKey: "fake-meta"}}, VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "fake-vertex"}}, OpenAICompatibility: []config.OpenAICompatibility{{Name: "compatible", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "fake-compat"}}}}}
	synth := NewConfigSynthesizer()
	run := func() []*coreauth.Auth {
		out, err := synth.Synthesize(&SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := run()
	cfg.GeminiKey[0].ConcurrencyLimit = &limit
	cfg.InteractionsKey[0].ConcurrencyLimit = &limit
	cfg.ClaudeKey[0].ConcurrencyLimit = &limit
	cfg.CodexKey[0].ConcurrencyLimit = &limit
	cfg.XAIKey[0].ConcurrencyLimit = &limit
	cfg.MetaKey[0].ConcurrencyLimit = &limit
	cfg.VertexCompatAPIKey[0].ConcurrencyLimit = &limit
	cfg.OpenAICompatibility[0].APIKeyEntries[0].ConcurrencyLimit = &limit
	after := run()
	if len(after) != 8 || len(before) != 8 {
		t.Fatalf("auth families before=%d after=%d", len(before), len(after))
	}
	for i, a := range after {
		if a.ID != before[i].ID || a.Attributes[coreauth.AttributeConcurrencyLimit] != "7" {
			t.Fatalf("limit must not change identity: %s %s %v", before[i].ID, a.ID, a.Attributes)
		}
	}
}
