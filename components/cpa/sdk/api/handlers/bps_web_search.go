package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type bpsSearchContext struct{ context.Context }

func (c bpsSearchContext) Value(key any) any {
	switch key.(type) {
	case pinnedAuthContextKey, selectedAuthCallbackContextKey, finalizedResponseFormatCallbackContextKey,
		originalRequestContextKey, preparedModelRouteContextKey, executionSessionContextKey:
		return nil
	}
	if key == "gin" || key == "cliproxy.roundtripper" {
		return nil
	}
	return c.Context.Value(key)
}

func (h *BaseAPIHandler) withBPSWebSearch(ctx context.Context, metadata ...map[string]any) context.Context {
	callerScope := callerScopeFromMetadata(requestExecutionMetadata(ctx))
	if len(metadata) > 0 {
		if scope := callerScopeFromMetadata(metadata[0]); scope != "" {
			callerScope = scope
		}
	}
	ctx = coreexecutor.WithBPSWebSearch(ctx, func(ctx context.Context, body []byte) ([]byte, error) {
		response, failure := h.executeBPSWebSearchResultForCaller(ctx, body, callerScope)
		if failure != nil {
			return nil, errors.New("BPS web search backend failed")
		}
		return response, nil
	})
	return coreexecutor.WithBPSWebSearchBackend(ctx, coreexecutor.BPSWebSearchBackend{
		Preflight: h.preflightBPSSearch,
		Execute: func(ctx context.Context, request coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
			if request.CallerScope == "" {
				request.CallerScope = callerScope
			}
			return h.executeBPSSearch(ctx, request)
		},
	})
}

func (h *BaseAPIHandler) executeBPSWebSearch(ctx context.Context, body []byte) ([]byte, error) {
	response, failure := h.executeBPSWebSearchResult(ctx, body)
	if failure != nil {
		return nil, errors.New("BPS web search backend failed")
	}
	return response, nil
}

func (h *BaseAPIHandler) executeBPSWebSearchResult(ctx context.Context, body []byte) ([]byte, *interfaces.ErrorMessage) {
	callerScope := callerScopeFromMetadata(requestExecutionMetadata(ctx))
	return h.executeBPSWebSearchResultForCaller(ctx, body, callerScope)
}

func (h *BaseAPIHandler) executeBPSWebSearchResultForCaller(ctx context.Context, body []byte, callerScope string) ([]byte, *interfaces.ErrorMessage) {
	// Keep cancellation and billing identity while isolating the nested provider selection.
	ctx = IsolateBPSAlphaSearchContext(ctx)
	response, failure := h.ExecuteModel(ctx, ModelExecutionRequest{
		EntryProtocol: "claude", ExitProtocol: "claude", Model: "cpa-bps-search-router",
		Body: body, Headers: http.Header{"Content-Type": []string{"application/json"}},
		Path: "/v1/messages", CallerScope: callerScope,
		SkipInterceptorPluginID: "cpa-bps", SkipRouterPluginID: "cpa-bps",
	})
	if failure != nil {
		return nil, failure
	}
	return response.Body, nil
}

// IsolateBPSAlphaSearchContext retains cancellation and usage identity without outer credential routing.
func IsolateBPSAlphaSearchContext(ctx context.Context) context.Context {
	ctx = usage.CaptureIdentity(ctx)
	ctx = coreauth.WithoutBPSExecutionState(bpsSearchContext{ctx})
	meta := logging.GetClientRequestMetadata(ctx)
	meta.SessionID, meta.ParentSessionID = "", ""
	meta.IsFork, meta.IsCompaction = false, false
	ctx = logging.WithClientRequestMetadata(ctx, meta)
	return EnrichContextWithSessionHierarchy(ctx, nil, nil, nil)
}
