package auth

import (
	"context"
	"errors"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
)

func noReplayProviderError(provider string, err error) error {
	if provider == "kilo" {
		return channelscope.PreventReplay(err)
	}
	return err
}

func noReplayCancellation(err error) bool {
	return channelscope.NoReplay(err) && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func (m *Manager) recordNoReplayCanceledResult(ctx context.Context, auth *Auth, provider, model, route string, opts core.Options, err error, ephemeral bool, seq uint64) bool {
	if provider == "kilo" && !core.UpstreamAttempted(ctx) {
		return true
	}
	if !channelscope.NoReplay(err) {
		return false
	}
	result := Result{AuthID: auth.ID, Provider: provider, Model: model, RouteModel: route, Success: false, Error: resultErrorFromError(err), Options: opts, RetryAfter: retryAfterFromError(err), AccessToken: authAccessTokenForRequest(ctx, auth, opts.Headers), CredentialVersion: auth.CredentialVersion, RegistrationEpoch: auth.RegistrationEpoch, ExecutionSeq: seq}
	result.CredentialScope = isCredentialScopedError(err)
	if noReplayCancellation(err) && !ephemeral {
		m.recordAvailabilityNeutralResult(context.WithoutCancel(ctx), result)
	} else {
		m.recordExecutionResult(context.WithoutCancel(ctx), result, auth, ephemeral)
	}
	return true
}
