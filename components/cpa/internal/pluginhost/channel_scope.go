package pluginhost

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// PluginExecutorChannel resolves channels from current host-registered executors.
func (h *Host) PluginExecutorChannel(pluginID string) (string, bool) {
	for _, record := range h.activeRecords() {
		if record.id == strings.TrimSpace(pluginID) && !h.isPluginFused(record.id) {
			return h.executorRecordChannel(record)
		}
	}
	return "", false
}

func (h *Host) executorRecordChannel(record capabilityRecord) (string, bool) {
	executor := record.plugin.Capabilities.Executor
	if executor == nil {
		return "", false
	}
	// The cpa-bps channel follows its host registration, not its provider identifier.
	if record.id == "cpa-bps" {
		return "bps", true
	}
	identifier, ok := h.callExecutorIdentifier(record.id, executor)
	if !ok {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(identifier)) {
	case "kilo", "codex", "bps":
		return strings.ToLower(strings.TrimSpace(identifier)), true
	default:
		return "", false
	}
}

func (a *executorAdapter) currentExecutionRecord() (capabilityRecord, bool) {
	if a == nil || a.host == nil || a.executor == nil || a.host.isPluginFused(a.pluginID) {
		return capabilityRecord{}, false
	}
	for _, record := range a.host.activeRecords() {
		if record.id != a.pluginID || record.path != a.path || record.version != a.version {
			continue
		}
		current := record.plugin.Capabilities.Executor
		if current != nil && reflect.ValueOf(current).Comparable() && current == a.executor {
			return record, true
		}
	}
	return capabilityRecord{}, false
}

func (a *executorAdapter) ExecutionChannel() (string, bool) {
	if a == nil || a.host == nil || !a.host.pluginIdentityCurrent(a.pluginID, a.path, a.version) {
		return "", false
	}
	if a.pluginID == "cpa-bps" {
		record, current := a.currentExecutionRecord()
		if !current {
			return "", false
		}
		return a.host.executorRecordChannel(record)
	}
	return a.host.PluginExecutorChannel(a.pluginID)
}

func (a *executorAdapter) checkChannelScope(ctx context.Context) error {
	scope := channelscope.From(ctx)
	channel, known := a.ExecutionChannel()
	if a.pluginID != "cpa-bps" || known {
		if scope == "" {
			if !known || channel != "kilo" {
				return nil
			}
		} else if known && channel == scope {
			return nil
		}
	}
	return &coreauth.Error{Code: "channel_scope", Message: "plugin execution is outside client channel", HTTPStatus: http.StatusForbidden}
}
