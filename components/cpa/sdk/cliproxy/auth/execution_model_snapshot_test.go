package auth

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestResolveExecutionModelWithConfig(t *testing.T) {
	old := &config.Config{OAuthModelAlias: map[string][]config.OAuthModelAlias{"codex": {{Name: "old-model", Alias: "search"}}}, CodexKey: []config.CodexKey{{APIKey: "synthetic", Models: []config.CodexModel{{Name: "old-api", Alias: "search"}}}}}
	next := old.CloneForRuntime()
	next.OAuthModelAlias["codex"][0].Name = "new-model"
	next.CodexKey[0].Models[0].Name = "new-api"
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(next)
	manager.SetOAuthModelAlias(next.OAuthModelAlias)
	for _, tc := range []struct {
		name             string
		auth             *Auth
		oldWant, newWant string
	}{
		{"oauth", &Auth{Provider: "codex", Prefix: "prefix", Metadata: map[string]any{"access_token": "synthetic"}}, "old-model(high)", "new-model(high)"},
		{"api_key", &Auth{Provider: "codex", Prefix: "prefix", Attributes: map[string]string{AttributeAPIKey: "synthetic"}}, "old-api(high)", "new-api(high)"},
		{"home", &Auth{Provider: "codex", Attributes: map[string]string{homeUpstreamModelAttributeKey: "home-model"}}, "home-model", "home-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := manager.ResolveExecutionModelWithConfig(old, tc.auth, "prefix/search(high)"); got != tc.oldWant {
				t.Fatalf("old snapshot=%q want=%q", got, tc.oldWant)
			}
			if got := manager.ResolveExecutionModelWithConfig(next, tc.auth, "prefix/search(high)"); got != tc.newWant {
				t.Fatalf("new snapshot=%q want=%q", got, tc.newWant)
			}
			if got := manager.ResolveExecutionModel(tc.auth, "prefix/search(high)"); got != tc.newWant {
				t.Fatalf("normal resolver changed=%q want=%q", got, tc.newWant)
			}
		})
	}
}
