package cliproxy

import (
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func serviceSDKConfig(cfg *config.Config) *config.SDKConfig {
	if cfg == nil {
		return nil
	}
	sdk := cfg.SDKConfig
	sdk.CodexOrphanDelegationCompatibility = cfg.Codex.OrphanDelegationCompatibility
	sdk.CodexResponseSteering = cfg.Codex.ResponseSteering
	if cfg.CommercialMode {
		sdk.RequestLog = false
	}
	return &sdk
}

func (s *Service) initializeModelExecutor() {
	h := handlers.NewBaseAPIHandlers(serviceSDKConfig(s.currentConfig()), s.coreManager)
	h.SetFullConfigSnapshot(s.currentConfig)
	if s.pluginHost != nil {
		h.SetPluginHost(s.pluginHost)
		s.pluginHost.SetModelExecutor(h)
		s.pluginHost.SetAuthManager(s.coreManager)
	}
	s.executionHandlers = h
}

// ModelExecutor returns the configured SDK execution entry without starting HTTP.
func (s *Service) ModelExecutor() *handlers.BaseAPIHandler {
	if s == nil {
		return nil
	}
	return s.executionHandlers
}
