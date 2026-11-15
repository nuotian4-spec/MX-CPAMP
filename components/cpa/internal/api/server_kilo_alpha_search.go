package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// kiloAlphaSearch handles only the authenticated Kilo channel. Other channels
// retain the existing Alpha path and its own channel admission rules.
func (s *Server) kiloAlphaSearch(c *gin.Context, body []byte) bool {
	if channelscope.From(c.Request.Context()) != "kilo" {
		providers := registry.GetGlobalRegistry().GetModelProviders(ojson.GetBytes(body, "model").String())
		knownKilo, allowed := false, false
		for _, provider := range providers {
			knownKilo = knownKilo || provider == "kilo"
			allowed = allowed || channelscope.Allows(c.Request.Context(), provider)
		}
		if knownKilo && !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "channel_mismatch", "message": "Alpha model is outside the authenticated channel"}})
			return true
		}
		return false
	}
	fail := func(status int, code string, err error, completed int) {
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error(), "completed_queries": completed}})
	}
	plan, err := helps.ParseKiloAlphaSearch(body)
	if err != nil {
		code := "kilo_alpha_invalid_request"
		var alpha *helps.KiloAlphaError
		if errors.As(err, &alpha) {
			code = alpha.Code
		}
		fail(400, code, err, 0)
		return true
	}
	ctx := context.WithValue(c.Request.Context(), "gin", c)
	ctx, target, err := s.handlers.PrepareKiloAlphaExecution(ctx, plan.Model, body)
	if err != nil {
		fail(400, "kilo_alpha_model_unavailable", err, 0)
		return true
	}
	plan.Model = target
	ctx = auth.WithSharedCredentialWaitBudget(auth.WithKiloSearchContext(ctx, plan.ID))
	var selected string
	ctx = handlers.WithSelectedAuthIDCallback(ctx, func(id string) {
		if selected == "" {
			selected = id
		}
	})
	outputs := make([]string, 0, len(plan.Queries))
	for i, query := range plan.Queries {
		doc, err := plan.Request(i)
		if err != nil {
			fail(400, "kilo_alpha_invalid_request", err, i)
			return true
		}
		raw := doc.Bytes()
		queryCtx := helps.WithKiloAlphaContract(handlers.WithOriginalRequest(ctx, raw), query, plan.MaxOutputTokens)
		response, failure := s.handlers.ExecuteModel(queryCtx, handlers.ModelExecutionRequest{EntryProtocol: "codex", ExitProtocol: "codex", Model: target, Body: raw, Headers: c.Request.Header.Clone(), ForcedProvider: "kilo", AuthID: selected, Path: c.Request.URL.Path})
		if failure != nil {
			for _, value := range failure.Headers.Values("Retry-After") {
				c.Header("Retry-After", value)
			}
			code := "kilo_alpha_execution_failed"
			var coded interface{ ErrorCode() string }
			if errors.As(failure.Error, &coded) && strings.HasPrefix(coded.ErrorCode(), "kilo_alpha_") {
				code = coded.ErrorCode()
			}
			fail(failure.StatusCode, code, failure.Error, i)
			return true
		}
		output, err := helps.KiloAlphaResult(response.Body)
		if err != nil {
			fail(clienterror.HTTPStatusFromErrorOr(err, http.StatusBadGateway), strings.SplitN(err.Error(), ":", 2)[0], err, i)
			return true
		}
		outputs = append(outputs, output)
	}
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("output", strings.Join(outputs, "\n\n"))
	c.Data(http.StatusOK, "application/json", doc.Bytes())
	return true
}
