package cliproxy

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type discoveryFixture struct {
	err    error
	models []pluginapi.ModelInfo
	before func()
	update pluginapi.AuthData
}

func (p *discoveryFixture) StaticModels(context.Context, pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
	return pluginapi.ModelResponse{Provider: "cpa-bps", Models: []pluginapi.ModelInfo{{ID: "fixture-model"}}}, nil
}
func (p *discoveryFixture) ModelsForAuth(context.Context, pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	if p.before != nil {
		p.before()
	}
	return pluginapi.ModelResponse{Provider: "cpa-bps", Models: p.models, AuthUpdate: p.update}, p.err
}

func TestPluginModelDiscoveryResults(t *testing.T) {
	m := coreauth.NewManager(nil, nil, nil)
	h := pluginhost.New()
	p := &discoveryFixture{err: errors.New("bps_model_discovery_rejected")}
	h.RegisterPluginForTest("cpa-bps", pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ModelProvider: p}})
	h.RegisterModels(t.Context(), registry.GetGlobalRegistry())
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient("plugin:cpa-bps:cpa-bps") })
	s := &Service{cfg: &config.Config{}, coreManager: m, pluginHost: h}
	a, err := m.Register(t.Context(), &coreauth.Auth{ID: t.Name(), Provider: "cpa-bps", Status: coreauth.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	reg := GlobalModelRegistry()
	t.Cleanup(func() { reg.UnregisterClient(a.ID) })
	reg.RegisterClient(a.ID, "cpa-bps", []*ModelInfo{{ID: "previous-model"}})
	s.tryRegisterPluginModelsForAuth(t.Context(), a, "cpa-bps", a.AuthKind(), nil)
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "failed" || got.ErrorCode != "bps_model_discovery_rejected" || got.PluginID != "cpa-bps" {
		t.Fatalf("failure: %+v", got)
	}
	if len(reg.GetModelsForClient(a.ID)) != 1 {
		t.Fatal("failure discarded existing directory")
	}
	p.err = errors.New("Bearer SECRET and account@example.invalid")
	s.tryRegisterPluginModelsForAuth(t.Context(), a, "cpa-bps", a.AuthKind(), nil)
	if got := m.ModelDiscoveryForAuth(a.ID); got.ErrorCode != "plugin_model_discovery_failed" {
		t.Fatalf("unsafe error: %+v", got)
	}
	p.err = nil
	s.tryRegisterPluginModelsForAuth(t.Context(), a, "cpa-bps", a.AuthKind(), nil)
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "succeeded" || got.ErrorCode != "" {
		t.Fatalf("empty success: %+v", got)
	}
	if len(reg.GetModelsForClient(a.ID)) != 0 {
		t.Fatal("empty success retained directory")
	}
	p.update = pluginapi.AuthData{Provider: "cpa-bps", Metadata: map[string]any{"access_token": "updated-test-token"}}
	p.models = []pluginapi.ModelInfo{{ID: "updated-model"}}
	s.tryRegisterPluginModelsForAuth(t.Context(), a, "cpa-bps", a.AuthKind(), nil)
	updated, ok := m.GetByID(a.ID)
	if !ok || updated.CredentialVersion <= a.CredentialVersion {
		t.Fatal("plugin credential update did not commit")
	}
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "succeeded" {
		t.Fatalf("successful auth update lost discovery: %+v", got)
	}
	if models := reg.GetModelsForClient(a.ID); len(models) != 1 || models[0].ID != "updated-model" {
		t.Fatal("updated models were not published")
	}
	a = updated
	p.update = pluginapi.AuthData{}
	p.before = func() {
		_, _, err = m.Modify(t.Context(), a.ID, a.RegistrationEpoch, func(current *coreauth.Auth) error { current.Disabled = true; return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	p.err = errors.New("bps_model_discovery_rejected")
	s.tryRegisterPluginModelsForAuth(t.Context(), a, "cpa-bps", a.AuthKind(), nil)
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "disabled" {
		t.Fatalf("late error overwrote disabled: %+v", got)
	}
}

func TestModelDiscoveryNewerFailureAfterOlderPublication(t *testing.T) {
	m := coreauth.NewManager(nil, nil, nil)
	a, err := m.Register(t.Context(), &coreauth.Auth{ID: t.Name(), Provider: "cpa-bps", Status: coreauth.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: &config.Config{}, coreManager: m}
	oldFence, newFence := newModelRegistrationFence(a, s.cfg), newModelRegistrationFence(a, s.cfg)
	oldAttempt, newAttempt := m.NextModelDiscoveryAttempt(), m.NextModelDiscoveryAttempt()
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(a.ID) })
	if !oldFence.publishModels(s, a.ID, "cpa-bps", []*ModelInfo{{ID: "existing"}}) {
		t.Fatal("publication failed")
	}
	s.recordPluginModelDiscovery(a, oldFence, oldAttempt, "cpa-bps", nil)
	s.recordPluginModelDiscovery(a, newFence, newAttempt, "cpa-bps", errors.New("bps_model_discovery_rejected"))
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "failed" {
		t.Fatalf("newer failure lost after epoch advanced: %+v", got)
	}
	s.recordPluginModelDiscovery(a, oldFence, oldAttempt, "cpa-bps", nil)
	if got := m.ModelDiscoveryForAuth(a.ID); got.Status != "failed" {
		t.Fatalf("older result replaced failure: %+v", got)
	}
}
