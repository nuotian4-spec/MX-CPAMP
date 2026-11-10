package auth

import (
	"maps"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// bindBPSToolHistory runs before admission, so a continuation waits only for its original credential.
func (m *Manager) bindBPSToolHistory(providers []string, req coreexecutor.Request, opts coreexecutor.Options) ([]string, coreexecutor.Options, error) {
	ids := bpsbinding.CallIDs(req.Payload)
	hasBPS := false
	for _, id := range ids {
		hasBPS = hasBPS || bpsbinding.IsCallID(id)
	}
	if !hasBPS {
		return providers, opts, nil
	}
	fail := func(message string) ([]string, coreexecutor.Options, error) {
		return nil, opts, NewRequestScopedError(message, 409)
	}
	providerAvailable := false
	for _, provider := range providers {
		providerAvailable = providerAvailable || strings.EqualFold(provider, "bps")
	}
	if !providerAvailable {
		return fail("BPS tool history requires its original BPS provider")
	}
	cfg, _ := m.runtimeConfig.Load().(*config.Config)
	if err := cfg.ValidateBPSProtocol(); err != nil {
		return fail(err.Error())
	}
	store := bpsbinding.Store{Dir: filepath.Join(config.BPSStateDirectory(cfg), "bindings")}
	caller, _ := opts.Metadata[coreexecutor.CallerScopeMetadataKey].(string)
	var original bpsbinding.Identity
	for _, id := range ids {
		record, err := store.Load(caller, id)
		if err != nil {
			return fail(err.Error())
		}
		if original.AuthID != "" && original != record.Identity {
			return fail("BPS tool history mixes different credentials")
		}
		original = record.Identity
	}
	current, ok := m.GetByID(original.AuthID)
	if !ok || current.Disabled || current.Status == StatusDisabled || current.Provider != "bps" || authMetadataString(current, "account_id") != original.AccountID || authMetadataString(current, "sub") != original.Subject {
		return fail(bpsbinding.ErrConflict.Error())
	}
	if pinned := pinnedAuthIDFromMetadata(opts.Metadata); pinned != "" && pinned != original.AuthID {
		return fail(bpsbinding.ErrConflict.Error())
	}
	opts.Metadata = maps.Clone(opts.Metadata)
	if opts.Metadata == nil {
		opts.Metadata = make(map[string]any)
	}
	opts.Metadata[coreexecutor.PinnedAuthMetadataKey] = original.AuthID
	return []string{"bps"}, opts, nil
}
