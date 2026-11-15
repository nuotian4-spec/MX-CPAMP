package auth

import (
	"context"
	"errors"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"net/http"
	"testing"
)

type jointBPSExecutor struct {
	provider, reason string
	ids              []string
}

func (e *jointBPSExecutor) Identifier() string { return e.provider }
func (e *jointBPSExecutor) Execute(_ context.Context, a *Auth, _ core.Request, _ core.Options) (core.Response, error) {
	e.ids = append(e.ids, a.ID)
	if e.provider == "bps" && len(e.ids) == 1 {
		return core.Response{}, classifiedFailoverFacts{failoverFacts{true, true}, "schema_validation", e.reason}
	}
	return core.Response{Payload: []byte(`{"ok":true}`)}, nil
}
func (e *jointBPSExecutor) CountTokens(ctx context.Context, a *Auth, r core.Request, o core.Options) (core.Response, error) {
	return e.Execute(ctx, a, r, o)
}
func (e *jointBPSExecutor) ExecuteStream(context.Context, *Auth, core.Request, core.Options) (*core.StreamResult, error) {
	return nil, errors.New("联合专项不调用流式")
}
func (e *jointBPSExecutor) Refresh(_ context.Context, a *Auth) (*Auth, error) { return a, nil }
func (e *jointBPSExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("联合专项不调用外网")
}
func TestBPSJointChannelRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, reason           string
		bpsAccounts, wantCalls int
		success                bool
	}{{"同通道一次恢复", "schema_mismatch", 2, 2, true}, {"资源拒绝不切", "number_budget_limit", 2, 1, false}, {"同通道无候选不跨通道", "schema_mismatch", 1, 1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(nil, nil, nil)
			m.SetConfig(&config.Config{AuthDir: t.TempDir()})
			m.SetRetryConfig(0, 0, 3)
			bps := &jointBPSExecutor{provider: "bps", reason: tc.reason}
			codex := &jointBPSExecutor{provider: "codex"}
			kilo := &jointBPSExecutor{provider: "kilo"}
			m.RegisterExecutor(bps)
			m.RegisterExecutor(codex)
			m.RegisterExecutor(kilo)
			model := t.Name()
			register := func(id, provider string) {
				id = t.Name() + id
				registry.GetGlobalRegistry().RegisterClient(id, provider, []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
				if _, err := m.Register(WithSkipPersist(context.Background()), &Auth{ID: id, Provider: provider, Status: StatusActive, Metadata: map[string]any{"account_id": id, "sub": id}}); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < tc.bpsAccounts; i++ {
				register(string(rune('A'+i)), "bps")
			}
			register("C", "codex")
			register("K", "kilo")
			ctx := channelscope.With(context.Background(), "bps")
			_, err := m.Execute(ctx, []string{"bps", "codex", "kilo"}, core.Request{Model: model, Payload: []byte(`{"input":"hi","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}`)}, core.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
			if (err == nil) != tc.success || len(bps.ids) != tc.wantCalls || len(codex.ids) != 0 || len(kilo.ids) != 0 {
				t.Fatalf("success=%v bps=%v codex=%v kilo=%v err=%v", err == nil, bps.ids, codex.ids, kilo.ids, err)
			}
			if len(bps.ids) == 2 && bps.ids[0] == bps.ids[1] {
				t.Fatal("同一凭证重放")
			}
			t.Logf("bps_dispatches=%d codex=0 kilo=0", len(bps.ids))
		})
	}
}
