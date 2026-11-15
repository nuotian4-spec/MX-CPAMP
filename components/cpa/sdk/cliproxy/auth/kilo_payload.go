package auth

import (
	"context"
	"errors"
	"slices"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// These upstream faults stay neutral even when configured actions rewrite Code.
func kiloPayloadRequestResult(result Result) bool {
	if result.Provider != "kilo" || result.Success || result.Error == nil {
		return false
	}
	value := ojson.Parse(result.Error.Message)
	return value.Get("upstream_status").Int() == 413 || value.Get("error.code").String() == "invalid_encrypted_content"
}

func (m *Manager) prepareKiloPayload(ctx context.Context, providers []string, req core.Request, opts core.Options) (context.Context, error) {
	if !channelscope.KiloPayload(ctx) || !slices.Contains(providers, "kilo") {
		return ctx, nil
	}
	if executor, ok := m.Executor("kilo"); ok {
		if preparer, ok := executor.(interface {
			PrepareKiloPayload(context.Context, core.Request, core.Options) (context.Context, error)
		}); ok {
			return preparer.PrepareKiloPayload(ctx, req, opts)
		}
	}
	return ctx, nil
}

func localKiloPayloadFailure(provider string, err error) bool {
	if provider != "kilo" || err == nil {
		return false
	}
	var code interface{ ErrorCode() string }
	if !errors.As(err, &code) {
		return false
	}
	return code.ErrorCode() == "kilo_request_too_large" || code.ErrorCode() == "kilo_image_preparation_busy"
}
