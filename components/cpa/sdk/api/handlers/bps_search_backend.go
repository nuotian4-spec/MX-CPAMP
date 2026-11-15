package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

// SetFullConfigSnapshot supplies immutable runtime configuration to nested execution.
func (h *BaseAPIHandler) SetFullConfigSnapshot(snapshot func() *config.Config) {
	h.runtimeMu.Lock()
	h.fullConfigSnapshot = snapshot
	h.runtimeMu.Unlock()
}

func (h *BaseAPIHandler) searchConfigSnapshot() *config.Config {
	h.runtimeMu.RLock()
	snapshot := h.fullConfigSnapshot
	h.runtimeMu.RUnlock()
	if snapshot == nil {
		return nil
	}
	cfg := snapshot()
	if cfg == nil {
		return nil
	}
	return cfg.CloneForRuntime()
}

func bpsSearchFailure(kind string, status int) *coreexecutor.BPSSearchError {
	return &coreexecutor.BPSSearchError{Kind: kind, Status: status}
}

func bpsSearchModel(cfg *config.Config, mode coreexecutor.BPSSearchMode) (string, error) {
	if mode == coreexecutor.BPSSearchLive {
		return "cpa-bps-search-router", nil
	}
	if cfg == nil || !cfg.Plugins.Enabled {
		return "", bpsSearchFailure(coreexecutor.BPSSearchBackendUnavailable, http.StatusServiceUnavailable)
	}
	router, exists := cfg.Plugins.Configs["claude-web-search-router"]
	var settings struct {
		CodexModel string `yaml:"codex_model"`
	}
	if !exists || router.Enabled == nil || !*router.Enabled || router.Raw.Decode(&settings) != nil {
		return "", bpsSearchFailure(coreexecutor.BPSSearchBackendUnavailable, http.StatusServiceUnavailable)
	}
	if mode != coreexecutor.BPSSearchCached && mode != coreexecutor.BPSSearchIndexed {
		return "", bpsSearchFailure(coreexecutor.BPSSearchPolicyRejected, http.StatusBadRequest)
	}
	model := strings.TrimSpace(settings.CodexModel)
	for _, provider := range registry.GetGlobalRegistry().GetModelProviders(thinking.ParseSuffix(model).ModelName) {
		if model != "" && provider == "codex" {
			return model, nil
		}
	}
	return "", bpsSearchFailure(coreexecutor.BPSSearchBackendUnavailable, http.StatusServiceUnavailable)
}

func (h *BaseAPIHandler) preflightBPSSearch(ctx context.Context, request coreexecutor.BPSSearchRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return h.checkBPSSearch(h.searchConfigSnapshot(), request)
}

func (h *BaseAPIHandler) checkBPSSearch(cfg *config.Config, request coreexecutor.BPSSearchRequest) error {
	if h.AuthManager == nil || cfg == nil {
		return bpsSearchFailure(coreexecutor.BPSSearchBackendUnavailable, http.StatusServiceUnavailable)
	}
	if !cfg.IsBPSProtocolEnabled() || !cfg.BPS.SearchEnabled() || request.RequireSources && request.Mode != coreexecutor.BPSSearchLive {
		return bpsSearchFailure(coreexecutor.BPSSearchPolicyRejected, http.StatusBadRequest)
	}
	if request.Mode == coreexecutor.BPSSearchLive {
		if !modelRoutersEnabled(h.modelRouterHost(), "cpa-bps") {
			return bpsSearchFailure(coreexecutor.BPSSearchBackendUnavailable, http.StatusServiceUnavailable)
		}
	}
	_, err := bpsSearchModel(cfg, request.Mode)
	return err
}

func (h *BaseAPIHandler) executeBPSSearch(ctx context.Context, request coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
	var result coreexecutor.BPSSearchResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	cfg := h.searchConfigSnapshot()
	if err := h.checkBPSSearch(cfg, request); err != nil {
		return result, err
	}
	if strings.TrimSpace(request.Query) == "" {
		return result, bpsSearchFailure(coreexecutor.BPSSearchPolicyRejected, http.StatusBadRequest)
	}
	if request.Mode == coreexecutor.BPSSearchLive {
		session := &helps.BPSSearchSession{Domains: request.AllowedDomains}
		body, failure := h.executeBPSWebSearchResultForCaller(ctx, session.Request(request.Query), request.CallerScope)
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if failure != nil {
			return result, classifyBPSSearchFailure(failure)
		}
		parsed, sources, err := session.ParseResult(body, request.Query)
		if err != nil {
			return result, bpsSearchFailure(coreexecutor.BPSSearchInvalidResponse, http.StatusBadGateway)
		}
		result.Text = ojson.GetBytes(parsed, "text").String()
		result.SourcesProvided = true
		for _, source := range sources {
			url, _ := source["url"].(string)
			title, _ := source["title"].(string)
			result.Sources = append(result.Sources, coreexecutor.BPSSearchSource{URL: url, Title: title})
		}
		return result, nil
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return result, bpsSearchFailure(coreexecutor.BPSSearchPolicyRejected, http.StatusBadRequest)
	}
	body := ojson.NewDocString(`{"model":"","commands":{},"settings":{}}`)
	_ = body.Set("id", request.SessionID)
	_ = body.Set("commands.search_query", []any{map[string]any{"q": request.Query}})
	if request.Mode == coreexecutor.BPSSearchCached {
		_ = body.Set("settings.external_web_access", false)
	} else {
		_ = body.Set("settings.external_web_access", "indexed")
	}
	if len(request.AllowedDomains) > 0 {
		_ = body.Set("settings.filters.allowed_domains", request.AllowedDomains)
	}
	response, failure := h.executeBPSAlphaBackend(ctx, cfg, body.Bytes(), request.CallerScope)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if failure != nil {
		return result, classifyBPSSearchFailure(failure)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, bpsSearchFailure(coreexecutor.BPSSearchHTTPRejected, response.StatusCode)
	}
	result.Text = ojson.GetBytes(response.Body, "output").String()
	return result, nil
}

func classifyBPSSearchFailure(failure *interfaces.ErrorMessage) error {
	if failure.Error != nil {
		var typed *coreexecutor.BPSSearchError
		if errors.As(failure.Error, &typed) {
			return typed
		}
		var authFailure *coreauth.Error
		if errors.As(failure.Error, &authFailure) && (authFailure.Code == "auth_not_found" || authFailure.Code == "auth_unavailable") {
			return bpsSearchFailure(coreexecutor.BPSSearchNoCredential, failure.StatusCode)
		}
	}
	if failure.StatusCode == http.StatusServiceUnavailable {
		return bpsSearchFailure(coreexecutor.BPSSearchBackendUnavailable, failure.StatusCode)
	}
	return bpsSearchFailure(coreexecutor.BPSSearchHTTPRejected, failure.StatusCode)
}

// ExecuteBPSAlphaBackend executes an already-admitted BPS standalone search.
func (h *BaseAPIHandler) ExecuteBPSAlphaBackend(ctx context.Context, cfg *config.Config, body []byte) (ModelExecutionResponse, *interfaces.ErrorMessage) {
	if cfg == nil {
		return ModelExecutionResponse{}, &interfaces.ErrorMessage{StatusCode: http.StatusServiceUnavailable, Error: bpsSearchFailure(coreexecutor.BPSSearchBackendUnavailable, http.StatusServiceUnavailable)}
	}
	return h.executeBPSAlphaBackend(ctx, cfg.CloneForRuntime(), body, callerScopeFromMetadata(requestExecutionMetadata(ctx)))
}

func (h *BaseAPIHandler) executeBPSAlphaBackend(ctx context.Context, cfg *config.Config, body []byte, callerScope string) (ModelExecutionResponse, *interfaces.ErrorMessage) {
	mode, err := helps.BPSAlphaSearchMode(body)
	if err != nil {
		return ModelExecutionResponse{}, &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: bpsSearchFailure(coreexecutor.BPSSearchPolicyRejected, http.StatusBadRequest)}
	}
	model, err := bpsSearchModel(cfg, coreexecutor.BPSSearchMode(mode))
	if err != nil {
		return ModelExecutionResponse{}, &interfaces.ErrorMessage{StatusCode: http.StatusServiceUnavailable, Error: err}
	}
	ctx = IsolateBPSAlphaSearchContext(ctx)
	body = RewriteCodexAlphaSearchModel(body, model)
	metadata := map[string]any{}
	addCallerScopeMetadata(metadata, callerScope)
	options := coreexecutor.Options{Headers: http.Header{}, OriginalRequest: body, Metadata: metadata}
	return h.ExecuteCodexAlphaSearch(ctx, cfg, CodexAlphaSearchRequest{Body: body, Model: model, Options: options, BPSBackend: true})
}
