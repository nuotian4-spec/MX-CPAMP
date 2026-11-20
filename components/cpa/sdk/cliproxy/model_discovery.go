package cliproxy

import (
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func pluginModelDiscoveryErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case "bps_model_discovery_rejected", "bps_model_discovery_failed", "bps_invalid_model_catalog", "bps_reauthorization_required", "bps_proxy_invalid":
		return err.Error()
	default:
		return "plugin_model_discovery_failed"
	}
}

func (s *Service) recordPluginModelDiscovery(a *coreauth.Auth, fence modelRegistrationFence, attempt uint64, pluginID string, err error) {
	if s.coreManager == nil {
		return
	}
	state := coreauth.ModelDiscovery{Status: "succeeded", PluginID: pluginID}
	if err != nil {
		state.Status, state.ErrorCode = "failed", pluginModelDiscoveryErrorCode(err)
	}
	fence.publishIfCurrent(s, a.ID, func() bool {
		return s.coreManager.RecordModelDiscovery(a, attempt, state)
	})
}
