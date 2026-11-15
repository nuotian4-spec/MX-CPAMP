package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func alphaSearchModelProvider(model string) string {
	providers := registry.GetGlobalRegistry().GetModelProviders(thinking.ParseSuffix(model).ModelName)
	for _, provider := range providers {
		if provider == "codex" {
			return "codex"
		}
	}
	for _, provider := range providers {
		if provider == "bps" || provider == "cpa-bps" {
			return provider
		}
	}
	return "codex"
}

func (s *Server) bpsAlphaSearch(ctx context.Context, c *gin.Context, provider, model string, body []byte, headers http.Header) {
	writeError := func(status int, code, message string) {
		doc := ojson.NewDocString(`{}`)
		_ = doc.Set("error", map[string]any{"code": code, "message": message, "type": "alpha_search_error"})
		c.Data(status, "application/json", doc.Bytes())
	}
	cfg := s.getConfig()
	if cfg == nil {
		writeError(http.StatusServiceUnavailable, "alpha_search_disabled", "BPS search configuration unavailable")
		return
	}
	if provider == "bps" && (!cfg.IsBPSProtocolEnabled() || !cfg.BPS.SearchEnabled()) {
		writeError(http.StatusServiceUnavailable, "alpha_search_disabled", "BPS web search is disabled")
		return
	}
	if provider == "cpa-bps" {
		plugin, exists := cfg.Plugins.Configs["cpa-bps"]
		var settings struct {
			WebSearch *bool `yaml:"web_search"`
		}
		errDecode := plugin.Raw.Decode(&settings)
		if !cfg.Plugins.Enabled || !exists || plugin.Enabled == nil || !*plugin.Enabled || errDecode != nil || (settings.WebSearch != nil && !*settings.WebSearch) {
			writeError(http.StatusServiceUnavailable, "alpha_search_disabled", "cpa-bps web search is disabled")
			return
		}
	}
	registered := false
	for _, candidate := range registry.GetGlobalRegistry().GetModelProviders(thinking.ParseSuffix(model).ModelName) {
		registered = registered || candidate == provider
	}
	if strings.TrimSpace(model) == "" || !registered {
		writeError(http.StatusServiceUnavailable, "alpha_search_model_unavailable", "BPS search model is not registered for the selected provider")
		return
	}
	mode, errMode := helps.BPSAlphaSearchMode(body)
	if errMode != nil {
		writeError(http.StatusUnprocessableEntity, "alpha_search_unsupported_request", errMode.Error())
		return
	}
	if mode != "live" {
		admitted, release, failure := s.handlers.AdmitBPSAlphaSearch(ctx, provider, model, body, headers)
		if failure != nil {
			writeError(http.StatusServiceUnavailable, "alpha_search_model_unavailable", "BPS search model admission failed")
			return
		}
		defer release()
		response, backendFailure := s.handlers.ExecuteBPSAlphaBackend(admitted, cfg, body)
		if backendFailure != nil {
			for _, value := range backendFailure.Headers.Values("Retry-After") {
				c.Writer.Header().Add("Retry-After", value)
			}
			code := "alpha_search_backend_unavailable"
			var searchFailure *coreexecutor.BPSSearchError
			if errors.As(backendFailure.Error, &searchFailure) && searchFailure.Kind == coreexecutor.BPSSearchInvalidResponse {
				code = "alpha_search_invalid_backend_response"
			}
			writeError(backendFailure.StatusCode, code, backendFailure.Error.Error())
			return
		}
		for name, values := range response.Headers {
			for _, value := range values {
				c.Writer.Header().Add(name, value)
			}
		}
		c.Status(response.StatusCode)
		_, _ = c.Writer.Write(response.Body)
		return
	}
	response, failure := s.handlers.ExecuteBPSAlphaSearch(ctx, provider, model, body, headers)
	if failure != nil {
		status := failure.StatusCode
		if status < 400 {
			status = http.StatusBadGateway
		}
		for _, value := range failure.Headers.Values("Retry-After") {
			c.Writer.Header().Add("Retry-After", value)
		}
		code := "alpha_search_backend_failed"
		if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
			code = "alpha_search_unsupported_request"
		} else if status == http.StatusServiceUnavailable {
			code = "alpha_search_backend_unavailable"
		}
		writeError(status, code, failure.Error.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", response)
}
