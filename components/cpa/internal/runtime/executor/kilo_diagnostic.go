package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func (*KiloExecutor) KiloDiagnosticModelAllowed(model string) bool {
	return model == "kilo-auto/free" || slices.Contains(helps.KiloModels, model)
}

// Auto Free is an internal text diagnostic, not the public Chat protocol.
func (e *KiloExecutor) executeAutoFreeDiagnostic(ctx context.Context, a *coreauth.Auth, req core.Request, opts core.Options) (result core.Response, err error) {
	reporter := helps.NewExecutorUsageReporter(ctx, e, req.Model, a)
	defer reporter.TrackFailure(ctx, &err)
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("model", "kilo-auto/free")
	_ = doc.Set("messages", []any{map[string]any{"role": "user", "content": ojson.GetBytes(req.Payload, "input").String()}})
	_ = doc.Set("stream", true)
	helps.NewPayloadFinalizer(cfg, "kilo", req.Model, translator.FormatOpenAI.String(), "", opts.OriginalRequest, req, opts, a).FinalizeDoc(doc)
	if doc.Get("model").String() != "kilo-auto/free" || !doc.Get("stream").Bool() || doc.Get("messages.#").Int() != 1 || doc.Get("messages.0.role").String() != "user" || doc.Get("messages.0.content").Type != ojson.String || strings.TrimSpace(doc.Get("messages.0.content").String()) == "" || doc.Get("tools").Exists() {
		return result, coreauth.NewRequestScopedError("invalid Kilo Auto Free diagnostic payload", 400)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, helps.KiloAutoFreeURL, bytes.NewReader(doc.Bytes()))
	if err != nil {
		return result, err
	}
	if err = e.PrepareRequest(request, a); err != nil {
		return result, err
	}
	if err = coreauth.RecordKiloDiagnosticCall(ctx); err != nil {
		return result, err
	}
	response, err := helps.KiloDo(ctx, a, request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return result, helps.KiloHTTPResponseError(response.StatusCode, body)
	}
	text, err := helps.ReadKiloAutoFree(ctx, response)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var incomplete *helps.KiloDiagnosticIncomplete
		var upstream *helps.KiloResponseError
		if errors.As(err, &incomplete) || errors.As(err, &upstream) {
			return result, err
		}
		return result, statusErr{code: 502, msg: "Kilo Auto Free response did not complete"}
	}
	out := ojson.NewDocString(`{"status":"completed"}`)
	_ = out.Set("output", []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}})
	return core.Response{Payload: out.Bytes()}, nil
}
