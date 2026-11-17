package auth

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

func KiloIdentity(a *Auth) kilobinding.Identity {
	if a == nil {
		return kilobinding.Identity{}
	}
	return kilobinding.Identity{DeviceProfileID: KiloDeviceProfileID(a), AuthID: a.ID, Provider: a.Provider, AccountID: authMetadataString(a, "account_id"), Subject: authMetadataString(a, "sub"), Organization: authMetadataString(a, "organization_id"), Revision: kiloIdentityRevision(a)}
}
func KiloBindingError(err error) error {
	code := 409
	if errors.Is(err, kilobinding.ErrStorage) {
		code = 503
		if operation, kind, systemCode, ok := kilobinding.StorageDiagnostic(err); ok {
			log.WithFields(log.Fields{"operation": operation, "error_type": kind, "system_code": systemCode}).Warn("Kilo binding storage operation failed")
		}
	}
	return NewRequestScopedError(err.Error(), code)
}

// bindKiloHistory resolves ownership before the existing account/egress admission.
func (m *Manager) bindKiloHistory(ctx context.Context, providers []string, req core.Request, opts core.Options) (context.Context, core.Options, func(), error) {
	noop := func() {}
	if !slices.Contains(providers, "kilo") {
		return ctx, opts, noop, nil
	}
	kilolatency.From(ctx).Enable(opts.Stream)
	cfg, _ := m.runtimeConfig.Load().(*config.Config)
	if err := cfg.ValidateKiloState(); err != nil {
		return ctx, opts, noop, NewRequestScopedError(err.Error(), 503)
	}
	caller, _ := opts.Metadata[core.CallerScopeMetadataKey].(string)
	if caller == "" {
		return ctx, opts, noop, KiloBindingError(kilobinding.ErrConflict)
	}
	raw := opts.OriginalRequest
	if len(raw) == 0 {
		raw = req.Payload
	}
	hints := kilobinding.ParseHints(opts.Headers, raw)
	if hints.Err != nil {
		return ctx, opts, noop, KiloBindingError(hints.Err)
	}
	var searchErr error
	hints, searchErr = kiloSearchBindingHints(ctx, hints, raw)
	if searchErr != nil {
		return ctx, opts, noop, searchErr
	}
	if hints.Session == "" {
		hints.Session, _ = opts.Metadata[core.ExecutionSessionMetadataKey].(string)
	}
	store := (kilobinding.Store{Dir: filepath.Join(config.KiloStateDirectory(cfg), "bindings")}).WithContext(ctx)
	release := noop
	if hints.Session != "" && (hints.Thread != "" || (hints.Previous == "" && len(hints.Calls) == 0)) {
		var err error
		release, err = kilobinding.LockWithWait(ctx, store.Dir+"\x00"+caller+"\x00"+hints.Session, func(waitCtx context.Context, ready <-chan struct{}) error {
			defer kilolatency.From(waitCtx).Measure("session_wait")()
			return m.waitCredentialCapacity(kilolatency.WithSessionWait(waitCtx), ready, "")
		})
		if err != nil {
			return ctx, opts, noop, err
		}
	}
	fail := func(err error) (context.Context, core.Options, func(), error) {
		release()
		return ctx, opts, noop, KiloBindingError(err)
	}
	record, err := store.Resolve(caller, hints)
	if err != nil {
		return fail(err)
	}
	sessionKey := hints.Session
	opts.Metadata = maps.Clone(opts.Metadata)
	if opts.Metadata == nil {
		opts.Metadata = map[string]any{}
	}
	if record.Identity.AuthID != "" {
		a, ok := m.GetByID(record.Identity.AuthID)
		if !ok || a.Disabled || a.Status == StatusDisabled || !kiloBindingIdentityMatches(a, record.Identity) {
			return fail(kilobinding.ErrConflict)
		}
		if pinned := pinnedAuthIDFromMetadata(opts.Metadata); pinned != "" && pinned != a.ID {
			return fail(kilobinding.ErrConflict)
		}
		opts.Metadata[core.PinnedAuthMetadataKey] = a.ID
		if record.ID != "" {
			opts.Metadata[core.CanonicalSessionIDMetadataKey] = record.ID
		} else {
			opts.Metadata[core.CanonicalSessionIDMetadataKey] = sessionKey
		}
	}
	plan := &kilobinding.Plan{Store: store, Caller: caller, SessionKey: sessionKey, PreferSession: hints.Thread == "" && hints.Previous != "", Record: record, Release: release}
	return kilobinding.WithPlan(ctx, plan), opts, release, nil
}

func kiloBindingIdentityMatches(a *Auth, expected kilobinding.Identity) bool {
	actual := KiloIdentity(a)
	required := expected.DeviceProfileID
	actual.DeviceProfileID, expected.DeviceProfileID = "", ""
	if actual != expected {
		return false
	}
	if required == "" {
		return true
	}
	profiles, err := KiloDeviceProfiles(a)
	if err != nil {
		return false
	}
	for _, profile := range profiles {
		if profile.ID == required {
			return true
		}
	}
	return false
}
