package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

const CodexAlphaSearchSourceFormat = "codex-alpha-search"

type CodexAlphaSearchRequest struct {
	Body              []byte
	Model             string
	Options           coreexecutor.Options
	Selected          *auth.Auth
	Selection         *auth.HomeDispatchSelection
	BPSBackend        bool
	SelectedAuthIndex func(string)
}

func SanitizeCodexAlphaSearchBody(body []byte) []byte {
	if !ojson.ValidBytes(body) || !ojson.ParseBytes(body).IsObject() {
		return body
	}
	doc := ojson.NewDoc(body)
	if !doc.Get("prompt_cache_key").Exists() && !doc.Get("prompt_cache_retention").Exists() {
		return body
	}
	_ = doc.Delete("prompt_cache_key")
	_ = doc.Delete("prompt_cache_retention")
	return doc.Bytes()
}

func RewriteCodexAlphaSearchModel(body []byte, model string) []byte {
	model = strings.TrimSpace(model)
	if model == "" || !ojson.ValidBytes(body) {
		return body
	}
	doc := ojson.NewDoc(body)
	if !doc.Get("model").Exists() || doc.Get("model").String() == model {
		return body
	}
	_ = doc.Set("model", model)
	return doc.Bytes()
}

// ExecuteCodexAlphaSearch owns credential selection and transport independently of HTTP handlers.
func (h *BaseAPIHandler) ExecuteCodexAlphaSearch(ctx context.Context, cfg *config.Config, request CodexAlphaSearchRequest) (ModelExecutionResponse, *interfaces.ErrorMessage) {
	var result ModelExecutionResponse
	fail := func(status int, kind string, err error) (ModelExecutionResponse, *interfaces.ErrorMessage) {
		if ctx.Err() != nil && (request.BPSBackend || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			err = ctx.Err()
			status = clienterror.HTTPStatusFromErrorOr(err, http.StatusRequestTimeout)
		} else if request.BPSBackend {
			err = bpsSearchFailure(kind, status)
		}
		return result, &interfaces.ErrorMessage{StatusCode: status, Error: err}
	}
	if err := ctx.Err(); err != nil {
		return fail(http.StatusRequestTimeout, coreexecutor.BPSSearchHTTPRejected, err)
	}
	if h.AuthManager == nil {
		return fail(http.StatusServiceUnavailable, coreexecutor.BPSSearchBackendUnavailable, errors.New("Codex auth manager unavailable"))
	}
	if !h.AuthManager.ProviderChannelAllowed(ctx, "codex") {
		return fail(http.StatusForbidden, coreexecutor.BPSSearchPolicyRejected, errors.New("no provider in client channel"))
	}
	body := SanitizeCodexAlphaSearchBody(request.Body)
	selected, selection := request.Selected, request.Selection
	var err error
	if selection != nil {
		selected = selection.CloneAuth()
	} else if selected == nil {
		if h.AuthManager.HomeEnabled() {
			selection, err = h.AuthManager.SelectHomeAuthWithCredentialPolicy(ctx, "codex", request.Model, auth.CredentialPolicyCodexAlphaSearchV1, request.Options)
			if selection != nil {
				selected = selection.CloneAuth()
			}
		} else {
			selected, err = h.AuthManager.SelectAuthWithCredentialPolicy(ctx, "codex", request.Model, auth.CredentialPolicyCodexAlphaSearchV1, request.Options)
		}
	}
	if selection != nil {
		defer selection.End("response_closed")
	}
	if err != nil {
		_, failure := fail(clienterror.HTTPStatusFromErrorOr(err, http.StatusServiceUnavailable), coreexecutor.BPSSearchNoCredential, err)
		failure.Headers = auth.SafeResponseHeaders(err)
		return result, failure
	}
	if selected == nil {
		return fail(http.StatusServiceUnavailable, coreexecutor.BPSSearchNoCredential, errors.New("Codex auth unavailable"))
	}
	if selection != nil {
		if selection.CanonicalSessionID != "" {
			meta := logging.GetClientRequestMetadata(ctx)
			meta.SessionID, meta.ParentSessionID = selection.CanonicalSessionID, selection.ParentSessionID
			if meta.SessionID == meta.ParentSessionID {
				meta.ParentSessionID = ""
			}
			ctx = logging.WithClientRequestMetadata(ctx, meta)
		}
		attemptCtx, release, errBind := selection.AttemptContext(ctx)
		if errBind != nil {
			return fail(http.StatusServiceUnavailable, coreexecutor.BPSSearchBackendUnavailable, errBind)
		}
		ctx = attemptCtx
		defer release()
	}
	if request.SelectedAuthIndex != nil {
		request.SelectedAuthIndex(selected.EnsureIndex())
	}
	headers := http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}, "Originator": []string{"codex_cli_rs"}}
	if !request.BPSBackend {
		for _, name := range []string{"Version", "User-Agent", "Session_id", "X-Client-Request-Id"} {
			if value := strings.TrimSpace(request.Options.Headers.Get(name)); value != "" {
				headers.Set(name, value)
			}
		}
	}
	if accountID, ok := selected.Metadata["account_id"].(string); ok && strings.TrimSpace(accountID) != "" {
		headers.Set("Chatgpt-Account-Id", accountID)
	}
	upstreamURL := "https://chatgpt.com/backend-api/codex/alpha/search"
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = strings.TrimSpace(ojson.GetBytes(body, "model").String())
	}
	if selected.AuthKind() == auth.AuthKindAPIKey {
		baseURL := strings.TrimSpace(selected.Attributes["base_url"])
		if baseURL == "" {
			return fail(http.StatusServiceUnavailable, coreexecutor.BPSSearchBackendUnavailable, errors.New("Codex Alpha Search API key base URL unavailable"))
		}
		upstreamURL = strings.TrimRight(baseURL, "/") + "/alpha/search"
		if !request.BPSBackend {
			body = RewriteCodexAlphaSearchModel(body, h.AuthManager.ResolveExecutionModel(selected, model))
		}
	}
	if request.BPSBackend {
		doc := ojson.NewDoc(body)
		if upstreamModel := h.AuthManager.ResolveExecutionModelWithConfig(cfg, selected, model); upstreamModel != "" {
			_ = doc.Set("model", upstreamModel)
		}
		helps.ApplyPayloadConfigWithRequestForExecutorDoc(cfg, "codex", model, CodexAlphaSearchSourceFormat, CodexAlphaSearchSourceFormat, "", doc, request.Body, model, "/v1/alpha/search", headers)
		requestedMode, _ := helps.BPSAlphaSearchMode(request.Body)
		finalMode, errMode := helps.BPSAlphaSearchMode(doc.Bytes())
		if errMode != nil || finalMode != requestedMode || !reflect.DeepEqual(ojson.GetBytes(request.Body, "settings.filters.allowed_domains").Value(), doc.Get("settings.filters.allowed_domains").Value()) || !reflect.DeepEqual(alphaQueryDomains(request.Body), alphaQueryDomains(doc.Bytes())) {
			return fail(http.StatusBadGateway, coreexecutor.BPSSearchPolicyRejected, errors.New("Alpha Search payload configuration changed a required search constraint"))
		}
		body = doc.Bytes()
	}
	req, err := h.AuthManager.NewHttpRequest(ctx, selected, http.MethodPost, upstreamURL, body, headers)
	if err != nil {
		return fail(clienterror.HTTPStatusFromErrorOr(err, http.StatusBadGateway), coreexecutor.BPSSearchHTTPRejected, err)
	}
	authType, authValue := selected.AccountInfo()
	logBody := body
	if request.BPSBackend {
		logBody = nil
	}
	helps.RecordAPIRequest(ctx, cfg, helps.UpstreamRequestLog{URL: upstreamURL, Method: http.MethodPost, Headers: req.Header.Clone(), Body: logBody, Provider: "codex", AuthID: selected.ID, AuthLabel: selected.Label, AuthType: authType, AuthValue: authValue})
	resp, err := h.AuthManager.HttpRequest(ctx, selected, req)
	if err != nil {
		helps.RecordAPIResponseError(ctx, cfg, err)
		return fail(clienterror.HTTPStatusFromErrorOr(err, http.StatusBadGateway), coreexecutor.BPSSearchHTTPRejected, err)
	}
	if selection != nil {
		if errBind := selection.Bind(resp.Body.Close); errBind != nil {
			if resp.StatusCode == http.StatusUnauthorized {
				h.AuthManager.ReportHomeUnauthorized(ctx, selected, "codex", request.Model)
			}
			return fail(http.StatusServiceUnavailable, coreexecutor.BPSSearchBackendUnavailable, errBind)
		}
	} else {
		defer resp.Body.Close()
	}
	stop := context.AfterFunc(ctx, func() { _ = resp.Body.Close() })
	defer stop()
	helps.RecordAPIResponseMetadata(ctx, cfg, resp.StatusCode, resp.Header.Clone())
	responseBody, errRead := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if !request.BPSBackend {
		helps.AppendAPIResponseChunk(ctx, cfg, responseBody)
	}
	if selection != nil && resp.StatusCode == http.StatusUnauthorized {
		h.AuthManager.ReportHomeUnauthorized(ctx, selected, "codex", request.Model, responseBody)
	}
	if err := ctx.Err(); err != nil {
		return fail(http.StatusRequestTimeout, coreexecutor.BPSSearchHTTPRejected, err)
	}
	if errRead != nil {
		helps.RecordAPIResponseError(ctx, cfg, errRead)
		return fail(clienterror.HTTPStatusFromErrorOr(errRead, http.StatusBadGateway), coreexecutor.BPSSearchInvalidResponse, errors.New("Failed to read Codex search response"))
	}
	if len(responseBody) > 32<<20 {
		return fail(http.StatusBadGateway, coreexecutor.BPSSearchInvalidResponse, errors.New("Codex search response exceeds the size limit"))
	}
	if request.BPSBackend && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		output, encrypted := ojson.GetBytes(responseBody, "output"), ojson.GetBytes(responseBody, "encrypted_output")
		if !ojson.ValidBytes(responseBody) || output.Type != ojson.String || encrypted.Exists() && encrypted.Raw != "null" && encrypted.Type != ojson.String {
			return fail(http.StatusBadGateway, coreexecutor.BPSSearchInvalidResponse, errors.New("Alpha Search backend returned an invalid response"))
		}
	}
	result = ModelExecutionResponse{StatusCode: resp.StatusCode, Headers: http.Header{}, Body: responseBody}
	for _, name := range []string{"Content-Type", "Retry-After"} {
		for _, value := range resp.Header.Values(name) {
			result.Headers.Add(name, value)
		}
	}
	return result, nil
}

func alphaQueryDomains(body []byte) []any {
	queries := ojson.GetBytes(body, "commands.search_query").Array()
	domains := make([]any, len(queries))
	for i, query := range queries {
		domains[i] = query.Get("domains").Value()
	}
	return domains
}
