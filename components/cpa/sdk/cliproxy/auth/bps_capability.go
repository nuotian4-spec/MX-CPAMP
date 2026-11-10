package auth

import (
	"context"
	"strings"

	"github.com/MoeForge/moego/ojson"

	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func validateBPSReplayBoundary(providers []string, req coreexecutor.Request, opts coreexecutor.Options) error {
	if previous := ojson.GetBytes(req.Payload, "previous_response_id").String(); strings.HasPrefix(previous, "resp_bps_") {
		return &Error{Code: ErrorCodeRequestScoped, HTTPStatus: 400, Message: "BPS requires complete text history; its response IDs cannot be continued"}
	}
	if !coreexecutor.HasAccountBoundHistory(req.Payload) || pinnedAuthIDFromMetadata(opts.Metadata) != "" {
		return nil
	}
	for _, provider := range providers {
		if strings.EqualFold(provider, "bps") {
			return &Error{Code: ErrorCodeRequestScoped, HTTPStatus: 400, Message: "Account-bound history has no original account binding; start a new session with complete text history"}
		}
	}
	return nil
}

// filterBPSProviders runs before account selection, including Home and mixed pools.
func filterBPSProviders(ctx context.Context, providers []string, req coreexecutor.Request, opts coreexecutor.Options) []string {
	exclude := coreexecutor.DownstreamWebsocket(ctx) || coreexecutor.RequiredUpstreamWebsocket(ctx) || opts.Alt != ""
	exclude = exclude || (opts.SourceFormat != sdktranslator.FormatOpenAI && opts.SourceFormat != sdktranslator.FormatOpenAIResponse && opts.SourceFormat != sdktranslator.FormatCodex)
	exclude = exclude || coreexecutor.BPSUnsupportedReason(req.Payload) != ""
	if !exclude {
		return providers
	}
	out := make([]string, 0, len(providers))
	for _, provider := range providers {
		if !strings.EqualFold(provider, "bps") {
			out = append(out, provider)
		}
	}
	return out
}
