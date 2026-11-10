package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	bpsauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/bps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type BPSExecutor struct{ cfg *config.Config }

type bpsRequestError struct{ message string }

func (e bpsRequestError) Error() string         { return e.message }
func (e bpsRequestError) StatusCode() int       { return http.StatusBadRequest }
func (e bpsRequestError) IsRequestScoped() bool { return true }

type bpsAcceptedResponseError struct{ message string }

func (e bpsAcceptedResponseError) Error() string           { return e.message }
func (e bpsAcceptedResponseError) StatusCode() int         { return http.StatusBadGateway }
func (e bpsAcceptedResponseError) IsStreamTruncated() bool { return true }

func init() {
	coreauth.RegisterRefreshLeadProvider("bps", func() *time.Duration { lead := 5 * time.Minute; return &lead })
}

func NewBPSExecutor(cfg *config.Config) *BPSExecutor { return &BPSExecutor{cfg: cfg} }
func (e *BPSExecutor) Identifier() string            { return "bps" }
func (e *BPSExecutor) RequestToFormat(coreexecutor.Request, coreexecutor.Options) sdktranslator.Format {
	return sdktranslator.FormatCodex
}

func (e *BPSExecutor) PrepareRequest(req *http.Request, auth *coreauth.Auth) error {
	if auth == nil || auth.Provider != "bps" {
		return statusErr{code: 401, msg: "invalid BPS credential"}
	}
	token, _ := auth.Metadata["access_token"].(string)
	accountID, _ := auth.Metadata["account_id"].(string)
	if err := helps.ApplyBPSHeaders(req, strings.TrimSpace(token), strings.TrimSpace(accountID)); err != nil {
		return statusErr{code: 401, msg: err.Error()}
	}
	return nil
}

func (e *BPSExecutor) HttpRequest(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("BPS request is nil")
	}
	if req.URL.String() != helps.BPSResponsesURL {
		return nil, errors.New("BPS credential request destination is invalid")
	}
	if err := e.PrepareRequest(req, auth); err != nil {
		return nil, err
	}
	return helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0).Do(req.WithContext(ctx))
}

func (e *BPSExecutor) build(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (*http.Request, []byte, error) {
	if coreexecutor.DownstreamWebsocket(ctx) || coreexecutor.RequiredUpstreamWebsocket(ctx) {
		return nil, nil, bpsRequestError{message: "BPS does not support WebSocket"}
	}
	if reason := coreexecutor.BPSUnsupportedReason(req.Payload); reason != "" {
		return nil, nil, bpsRequestError{message: reason}
	}
	if opts.Alt != "" {
		return nil, nil, bpsRequestError{message: "BPS does not support this operation"}
	}
	if opts.SourceFormat != sdktranslator.FormatOpenAI && opts.SourceFormat != sdktranslator.FormatOpenAIResponse && opts.SourceFormat != sdktranslator.FormatCodex {
		return nil, nil, bpsRequestError{message: "BPS supports OpenAI text requests only"}
	}
	model := thinking.ParseSuffix(req.Model).ModelName
	body, err := helps.TranslateRequestReturningError(ctx, opts.Headers, e.cfg, opts.SourceFormat, sdktranslator.FormatCodex, model, bytes.Clone(req.Payload), true, false)
	if err != nil {
		return nil, nil, err
	}
	doc := ojson.NewDoc(body)
	if err = helps.ApplyRequestThinkingDoc(doc, req, opts, opts.SourceFormat.String(), sdktranslator.FormatCodex.String(), e.Identifier()); err != nil {
		return nil, nil, err
	}
	if err = helps.PrepareBPSDoc(doc, model); err != nil {
		return nil, nil, bpsRequestError{message: err.Error()}
	}
	original := opts.OriginalRequest
	if len(original) == 0 {
		original = req.Payload
	}
	finalize := helps.NewPayloadFinalizer(e.cfg, e.Identifier(), model, sdktranslator.FormatCodex.String(), "", original, req, opts, auth)
	finalize.FinalizeDoc(doc)
	body = doc.Bytes()
	// Validation after user rules is read-only; it never restores filtered fields.
	if reason := coreexecutor.BPSUnsupportedReason(body); reason != "" {
		return nil, nil, bpsRequestError{message: reason}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, helps.BPSResponsesURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	if err = e.PrepareRequest(httpReq, auth); err != nil {
		return nil, nil, err
	}
	return httpReq, body, nil
}

func (e *BPSExecutor) send(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options, reporter *helps.UsageReporter) (*http.Response, []byte, error) {
	httpReq, body, err := e.build(ctx, auth, req, opts)
	if err != nil {
		return nil, nil, err
	}
	client := reporter.TrackHTTPClient(helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0))
	response, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		return nil, nil, statusErr{code: response.StatusCode, msg: fmt.Sprintf("BPS upstream returned HTTP %d", response.StatusCode)}
	}
	return response, body, nil
}

func (e *BPSExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (result coreexecutor.Response, err error) {
	reporter := helps.NewExecutorUsageReporter(ctx, e, req.Model, auth)
	defer reporter.TrackFailure(ctx, &err)
	response, body, err := e.send(ctx, auth, req, opts, reporter)
	if err != nil {
		return result, err
	}
	defer func() { _ = response.Body.Close() }()
	var terminal []byte
	err = helps.ReadBPSEvents(ctx, response, func(event []byte) error {
		if ojson.GetBytes(event, "type").String() == "response.completed" {
			terminal = bytes.Clone(event)
		}
		return nil
	})
	if err != nil {
		return result, bpsAcceptedResponseError{message: err.Error()}
	}
	if detail, ok := helps.ParseCodexUsage(terminal); ok {
		reporter.Publish(ctx, detail)
	}
	var parameter any
	payload := sdktranslator.TranslateNonStream(ctx, sdktranslator.FormatCodex, coreexecutor.ResponseFormatOrSource(opts), req.Model, opts.OriginalRequest, body, terminal, &parameter)
	if len(payload) == 0 {
		return result, statusErr{code: 502, msg: "BPS response translation failed"}
	}
	return coreexecutor.Response{Payload: payload, Headers: response.Header.Clone()}, nil
}

func (e *BPSExecutor) ExecuteStream(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (_ *coreexecutor.StreamResult, err error) {
	reporter := helps.NewExecutorUsageReporter(ctx, e, req.Model, auth)
	reporter.SetStream(true)
	response, body, err := e.send(ctx, auth, req, opts, reporter)
	if err != nil {
		reporter.PublishFailure(ctx, err)
		return nil, err
	}
	chunks := make(chan coreexecutor.StreamChunk)
	go func() {
		defer close(chunks)
		defer func() { _ = response.Body.Close() }()
		var parameter any
		started := false
		var lifecycle helps.BPSStreamLifecycle
		emit := func(event []byte) error {
			line := append([]byte("data: "), event...)
			translated := sdktranslator.TranslateStream(ctx, sdktranslator.FormatCodex, coreexecutor.ResponseFormatOrSource(opts), req.Model, opts.OriginalRequest, body, line, &parameter)
			for _, payload := range translated {
				select {
				case chunks <- coreexecutor.StreamChunk{Payload: payload}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		streamErr := helps.ReadBPSEvents(ctx, response, func(event []byte) error {
			if detail, ok := helps.ParseCodexUsage(event); ok {
				reporter.Publish(ctx, detail)
			}
			if !started {
				if ojson.GetBytes(event, "type").String() != "response.created" {
					id := ojson.GetBytes(event, "response_id").String()
					if id == "" {
						id = ojson.GetBytes(event, "response.id").String()
					}
					if id == "" {
						return errors.New("BPS stream has no response identity")
					}
					created := ojson.NewDocString(`{"type":"response.created","response":{"object":"response","status":"in_progress","output":[]}}`)
					_ = created.Set("response.id", id)
					if errEmit := emit(created.Bytes()); errEmit != nil {
						return errEmit
					}
				}
				started = true
			}
			events, errEvents := lifecycle.Events(event)
			if errEvents != nil {
				return errEvents
			}
			for _, publicEvent := range events {
				if errEmit := emit(publicEvent); errEmit != nil {
					return errEmit
				}
			}
			return nil
		})
		if streamErr != nil {
			reporter.PublishFailure(ctx, streamErr)
			select {
			case chunks <- coreexecutor.StreamChunk{Err: bpsAcceptedResponseError{message: streamErr.Error()}}:
			case <-ctx.Done():
			}
		}
	}()
	return &coreexecutor.StreamResult{Headers: response.Header.Clone(), Chunks: chunks}, nil
}

func (e *BPSExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	if auth == nil || auth.Provider != "bps" {
		return nil, errors.New("invalid BPS credential")
	}
	proxy := auth.ProxyURL
	if proxy == "" && e.cfg != nil {
		proxy = e.cfg.ProxyURL
	}
	metadata, err := bpsauth.NewClient(proxy).Refresh(ctx, auth.Metadata)
	if err != nil {
		return nil, err
	}
	updated := auth.Clone()
	updated.Metadata = metadata
	updated.LastRefreshedAt = time.Now().UTC()
	return updated, nil
}

func (e *BPSExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, bpsRequestError{message: "BPS token counting is not supported"}
}
