package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// AdmitBPSAlphaSearch checks the original model without executing or recording a BPS result.
func (h *BaseAPIHandler) AdmitBPSAlphaSearch(ctx context.Context, provider, model string, body []byte, headers http.Header) (context.Context, func(), *interfaces.ErrorMessage) {
	options := coreexecutor.Options{
		Headers: headers, OriginalRequest: body, Metadata: requestExecutionMetadata(ctx),
	}
	if h.AuthManager.HomeEnabled() {
		selection, errSelect := h.AuthManager.SelectHomeAuthByKind(ctx, provider, model, coreauth.AuthKindOAuth, options)
		if errSelect != nil {
			return ctx, func() {}, &interfaces.ErrorMessage{StatusCode: clienterror.HTTPStatusFromErrorOr(errSelect, http.StatusServiceUnavailable), Error: errSelect}
		}
		attempt, release, errAttempt := selection.AttemptContext(ctx)
		if errAttempt != nil {
			selection.End("alpha_admission_bind_failed")
			return ctx, func() {}, &interfaces.ErrorMessage{StatusCode: http.StatusServiceUnavailable, Error: errAttempt}
		}
		return attempt, func() { release(); selection.End("alpha_admission_finished") }, nil
	}
	_, errSelect := h.AuthManager.SelectAuth(ctx, provider, model, options)
	if errSelect != nil {
		return ctx, func() {}, &interfaces.ErrorMessage{StatusCode: clienterror.HTTPStatusFromErrorOr(errSelect, http.StatusServiceUnavailable), Error: errSelect}
	}
	return ctx, func() {}, nil
}

func (h *BaseAPIHandler) ExecuteBPSAlphaSearch(ctx context.Context, provider, model string, body []byte, headers http.Header) ([]byte, *interfaces.ErrorMessage) {
	plan, errPlan := helps.PrepareBPSAlphaSearch(body)
	if errPlan != nil {
		return nil, &interfaces.ErrorMessage{StatusCode: http.StatusUnprocessableEntity, Error: errPlan}
	}
	ctx, release, failure := h.AdmitBPSAlphaSearch(ctx, provider, model, body, headers)
	if failure != nil {
		return nil, failure
	}
	defer release()
	var outputs []string
	for i, request := range plan.Requests {
		if errContext := ctx.Err(); errContext != nil {
			return nil, executionErrorMessage(errContext)
		}
		response, failure := h.executeBPSWebSearchResult(ctx, request)
		if failure != nil {
			if ojson.Get(failure.Error.Error(), "error.code").String() == "model_not_found" || strings.Contains(failure.Error.Error(), "auth_not_found") {
				return nil, &interfaces.ErrorMessage{StatusCode: http.StatusServiceUnavailable, Error: errors.New("No available backend for the configured BPS web search router")}
			}
			return nil, failure
		}
		if errContext := ctx.Err(); errContext != nil {
			return nil, executionErrorMessage(errContext)
		}
		output, errOutput := plan.Output(i, response)
		if errOutput != nil {
			return nil, &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: errors.New("Alpha Search backend returned an invalid web search result")}
		}
		outputs = append(outputs, output)
	}
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("output", strings.Join(outputs, "\n\n"))
	return doc.Bytes(), nil
}
