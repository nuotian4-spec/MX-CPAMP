package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	device "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	translator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// KiloExecutor keeps one proxy runtime across all Kilo credentials.
type KiloExecutor struct {
	mu           sync.RWMutex
	cfg          *config.Config
	runtime      *helps.KiloRuntime
	initErr      error
	active       int
	pending      *config.Config
	changed      chan struct{}
	profileStore kilo.DeviceProfileStore
}

func NewKiloExecutor(cfg *config.Config) *KiloExecutor {
	if cfg == nil {
		cfg = &config.Config{}
	}
	runtime, err := helps.NewManagedKiloRuntime(cfg)
	e := &KiloExecutor{cfg: cfg, runtime: runtime, initErr: err, changed: make(chan struct{})}
	if runtime != nil {
		runtime.SetWake(e.notify)
		runtime.SetObservationCommit(e.observationCommit(runtime))
	}
	return e
}
func (*KiloExecutor) Identifier() string { return "kilo" }
func (e *KiloExecutor) PrepareKiloPayload(ctx context.Context, req core.Request, opts core.Options) (context.Context, error) {
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	enabled := cfg != nil && cfg.Kilo.ImageReductionEnabled &&
		!helps.KiloPayloadHasConditionalRules(cfg, thinking.ParseSuffix(req.Model).ModelName, req, opts)
	return helps.PrepareKiloPayload(ctx, req, opts, enabled)
}
func (*KiloExecutor) RequestToFormat(core.Request, core.Options) translator.Format {
	return translator.FormatCodex
}
func (e *KiloExecutor) UpdateConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtime != nil && e.runtime.StateView().Managed && config.KiloStateDirectory(cfg) != e.runtime.StateDirectory() {
		e.initErr = kilo.ErrStateStorage
		e.notifyLocked()
		return
	}
	e.cfg = cfg
	if e.runtime != nil && (e.runtime.StateView().Managed || e.runtime.Matches(cfg.Kilo)) {
		e.pending = nil
		e.initErr = nil
		e.notifyLocked()
		return
	}
	e.pending = cfg
	if e.active == 0 {
		e.applyPendingLocked()
	}
	e.notifyLocked()
}
func (e *KiloExecutor) notify() { e.mu.Lock(); defer e.mu.Unlock(); e.notifyLocked() }
func (e *KiloExecutor) notifyLocked() {
	close(e.changed)
	e.changed = make(chan struct{})
}
func (e *KiloExecutor) applyPendingLocked() {
	if e.pending == nil {
		return
	}
	cfg := e.pending
	e.pending = nil
	next, err := helps.NewManagedKiloRuntime(cfg)
	if err != nil {
		e.initErr = err
		return
	}
	if e.runtime != nil {
		next.InheritHealth(e.runtime)
		e.runtime.CloseIdleConnections()
	}
	next.SetDeviceProfileStore(e.profileStore)
	next.SetWake(e.notify)
	next.SetObservationCommit(e.observationCommit(next))
	e.runtime = next
	e.initErr = nil
}
func (e *KiloExecutor) EgressChanges() <-chan struct{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.changed
}
func (e *KiloExecutor) TryAcquireEgress(a *coreauth.Auth) (*coreauth.Auth, func(), bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pending != nil {
		return nil, nil, true, nil
	}
	if e.initErr != nil || e.runtime == nil {
		return nil, nil, false, coreauth.NewRequestScopedError("Kilo proxy configuration is unavailable", 503)
	}
	admitted, release, wait, err := e.runtime.TryAcquire(a)
	if err != nil || wait {
		return admitted, release, wait, err
	}
	e.active++
	var once sync.Once
	return admitted, func() {
		once.Do(func() {
			release()
			e.mu.Lock()
			defer e.mu.Unlock()
			e.active--
			if e.active == 0 {
				e.applyPendingLocked()
			}
			e.notifyLocked()
		})
	}, false, nil
}
func (e *KiloExecutor) KiloProxySnapshot() kilo.PoolSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.runtime == nil {
		return kilo.PoolSnapshot{}
	}
	return e.runtime.Snapshot()
}

func (e *KiloExecutor) acquireDevice(owner, mode string) (*http.Client, func(), error) {
	e.mu.Lock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		e.mu.Unlock()
		return nil, nil, device.DeviceUnavailable()
	}
	client, release, err := e.runtime.AcquireDevice(owner, mode)
	if err != nil {
		e.mu.Unlock()
		return nil, nil, err
	}
	e.active++
	e.mu.Unlock()
	return client, func() {
		release()
		e.mu.Lock()
		defer e.mu.Unlock()
		e.active--
		if e.active == 0 {
			e.applyPendingLocked()
		}
		e.notifyLocked()
	}, nil
}
func (e *KiloExecutor) StartKiloDevice(ctx context.Context, owner, mode string) (device.DeviceCode, error) {
	client, release, err := e.acquireDevice(owner, mode)
	if err != nil {
		return device.DeviceCode{}, err
	}
	defer release()
	return device.StartDevice(ctx, client)
}
func (e *KiloExecutor) PollKiloDevice(ctx context.Context, owner, mode, code string) (device.DeviceApproval, error) {
	client, release, err := e.acquireDevice(owner, mode)
	if err != nil {
		return device.DeviceApproval{}, err
	}
	defer release()
	return device.PollDevice(ctx, client, code)
}
func (e *KiloExecutor) KiloDeviceSaveEgress(accountIDs ...string) (string, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.runtime == nil {
		return "pending", "shared_proxy_not_configured"
	}
	return e.runtime.DeviceSaveEgress(accountIDs...)
}

// acquireProfile waits only for health admission within one total budget.
// No executor lock or business request is held while the health probe runs.
func (e *KiloExecutor) acquireProfile(ctx context.Context, accountID string) (*http.Client, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err := waitCtx.Err(); err != nil {
			return nil, nil, err
		}
		e.mu.Lock()
		if e.initErr != nil || e.runtime == nil || e.pending != nil {
			e.mu.Unlock()
			return nil, nil, device.ProfileUnavailable()
		}
		changed := e.changed
		client, release, err := e.runtime.AcquireProfile(accountID)
		if errors.Is(err, helps.ErrKiloProfileHealthPending) {
			e.mu.Unlock()
			select {
			case <-waitCtx.Done():
				return nil, nil, waitCtx.Err()
			case <-changed:
				continue
			}
		}
		if err != nil {
			e.mu.Unlock()
			return nil, nil, err
		}
		e.active++
		e.mu.Unlock()
		return client, func() {
			release()
			e.mu.Lock()
			defer e.mu.Unlock()
			e.active--
			if e.active == 0 {
				e.applyPendingLocked()
			}
			e.notifyLocked()
		}, nil
	}
}

// KiloOrganizations holds no executor/configuration lock during network I/O.
func (e *KiloExecutor) KiloOrganizations(ctx context.Context, a *coreauth.Auth) (device.OrganizationChoices, error) {
	if a == nil {
		return device.OrganizationChoices{}, device.ProfileUnavailable()
	}
	client, release, err := e.acquireProfile(ctx, a.ID)
	if err != nil {
		return device.OrganizationChoices{}, device.ProfileUnavailable()
	}
	defer release()
	token, _ := a.Metadata["api_key"].(string)
	if token == "" {
		token, _ = a.Metadata["access_token"].(string)
	}
	return device.FetchProfile(ctx, client, token)
}

// KiloUsage shares the existing account egress pool, not inference admission.
func (e *KiloExecutor) KiloUsage(ctx context.Context, accountID, token, outerAccountID string) (device.UsageObservation, error) {
	client, release, err := e.acquireProfile(ctx, accountID)
	if err != nil {
		return device.UsageObservation{}, device.UsageError("egress_unavailable")
	}
	defer release()
	return device.FetchUsage(ctx, client, token, outerAccountID)
}
func (e *KiloExecutor) KiloProxyState() (kilo.ProxyStateView, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.initErr != nil || e.runtime == nil {
		return kilo.ProxyStateView{}, kilo.ErrStateStorage
	}
	return e.runtime.StateView(), nil
}

// KiloDeviceProfileBindings copies published relations without account callbacks.
func (e *KiloExecutor) KiloDeviceProfileBindings() (map[string]map[string]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.initErr != nil || e.runtime == nil {
		return nil, kilo.ErrStateStorage
	}
	return e.runtime.DeviceProfileBindings(), nil
}

func (e *KiloExecutor) PreviewKiloProxy(ctx context.Context, raw string) (kilo.ProxyPreview, error) {
	e.mu.RLock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		e.mu.RUnlock()
		return kilo.ProxyPreview{}, kilo.ErrStateStorage
	}
	runtime := e.runtime
	e.mu.RUnlock()
	ip, probeErr := helps.ProbeKiloProxyObservation(ctx, raw)
	e.mu.RLock()
	defer e.mu.RUnlock()
	if ctx.Err() != nil {
		return kilo.ProxyPreview{}, ctx.Err()
	}
	if e.runtime != runtime || e.initErr != nil || e.pending != nil {
		return kilo.ProxyPreview{}, kilo.ErrStateConflict
	}
	return runtime.PreviewProxyObservation(ip, probeErr), nil
}

func (e *KiloExecutor) ProbeKiloProxy(ctx context.Context, revision uint64, id string, restore bool) (kilo.ProxyStateView, error) {
	e.mu.Lock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		e.mu.Unlock()
		return kilo.ProxyStateView{}, kilo.ErrStateStorage
	}
	runtime := e.runtime
	proxy, err := runtime.ProxyForProbe(revision, id, restore)
	e.mu.Unlock()
	if err != nil {
		return kilo.ProxyStateView{}, err
	}
	observedIP, probeErr := helps.ProbeKiloProxyObservation(ctx, proxy.URL)
	e.mu.Lock()
	defer e.mu.Unlock()
	if ctx.Err() != nil {
		return kilo.ProxyStateView{}, ctx.Err()
	}
	if e.runtime != runtime || e.pending != nil || e.initErr != nil {
		return kilo.ProxyStateView{}, kilo.ErrStateConflict
	}
	view, err := runtime.CompleteProxyProbe(revision, proxy, restore, probeErr, observedIP)
	e.notifyLocked()
	return view, err
}
func (e *KiloExecutor) PrepareKiloProxyChange(c kilo.ProxyChanges) (kilo.ProxyChangeObserve, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		return nil, kilo.ErrStateStorage
	}
	runtime := e.runtime
	expected, err := runtime.ProxyForChange(c)
	if err != nil {
		return nil, err
	}
	observe := c.Create || expected.URL != *c.URL
	raw := *c.URL
	c.URL = &raw
	return func(ctx context.Context) (kilo.ProxyChangeCommit, error) {
		var ip string
		var probeErr error
		if observe {
			ip, probeErr = helps.ProbeKiloProxyObservation(ctx, raw)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return func(accounts map[string]kilo.EgressAccountStatus) (kilo.ProxyStateView, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if ctx.Err() != nil {
				return kilo.ProxyStateView{}, ctx.Err()
			}
			if e.runtime != runtime || e.initErr != nil || e.pending != nil {
				return kilo.ProxyStateView{}, kilo.ErrStateConflict
			}
			current, err := runtime.ProxyForChange(c)
			if err != nil {
				return kilo.ProxyStateView{}, err
			}
			if current != expected {
				return kilo.ProxyStateView{}, kilo.ErrStateConflict
			}
			var view kilo.ProxyStateView
			if observe {
				view, err = runtime.ChangeObservedState(c, accounts, kilo.ProxyChangeObservation{IP: ip, Err: probeErr})
			} else {
				view, err = runtime.ChangeState(c, accounts)
			}
			if err == nil {
				e.notifyLocked()
			}
			return view, err
		}, nil
	}, nil
}

func (e *KiloExecutor) ChangeKiloProxyState(c kilo.ProxyChanges, accounts map[string]kilo.EgressAccountStatus) (kilo.ProxyStateView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		return kilo.ProxyStateView{}, kilo.ErrStateStorage
	}
	view, err := e.runtime.ChangeState(c, accounts)
	if err == nil {
		e.notifyLocked()
	}
	return view, err
}
func (e *KiloExecutor) Refresh(_ context.Context, a *coreauth.Auth) (*coreauth.Auth, error) {
	return a, nil
}
func (e *KiloExecutor) CountTokens(context.Context, *coreauth.Auth, core.Request, core.Options) (core.Response, error) {
	return core.Response{}, coreauth.NewRequestScopedError("Kilo token counting is not supported", 501)
}
func (e *KiloExecutor) SupportsKiloAlphaSearch(a *coreauth.Auth, model string) bool {
	return a != nil && a.Provider == "kilo" && strings.TrimSpace(model) != ""
}
func (e *KiloExecutor) SupportsNativeToolSearch(a *coreauth.Auth, model string) bool {
	return e.SupportsKiloAlphaSearch(a, model)
}
func (e *KiloExecutor) PrepareRequest(req *http.Request, a *coreauth.Auth) error {
	if a == nil || a.Provider != "kilo" {
		return statusErr{code: 401, msg: "invalid Kilo credential"}
	}
	token, _ := a.Metadata["api_key"].(string)
	if token == "" {
		token, _ = a.Metadata["access_token"].(string)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return statusErr{code: 401, msg: "Kilo credential is missing"}
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(token, "Bearer "))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "Kilo-Code/7.8.27 ai-sdk/provider-utils/4.0.40 runtime/bun/1.4.4")
	req.Header.Set("X-KILOCODE-EDITORNAME", "Kilo CLI 7.8.27")
	req.Header.Set("X-KILOCODE-FEATURE", "cli")
	machine := coreauth.KiloMachineID(a)
	if machine == "" {
		return statusErr{code: 503, msg: "Kilo machine identity is not persisted"}
	}
	req.Header.Set("X-KILOCODE-MACHINEID", machine)
	req.Header.Set("x-kilocode-mode", "code")
	session, err := helps.CommitKiloBinding(req.Context(), a)
	if err != nil {
		return err
	}
	for _, header := range []string{"X-KILOCODE-TASKID", "X-Session-Id", "x-session-affinity"} {
		req.Header.Set(header, session)
	}
	req.Header.Del("X-KiloCode-OrganizationId")
	if org, _ := a.Metadata["organization_id"].(string); org != "" {
		req.Header.Set("X-KiloCode-OrganizationId", org)
	}
	return nil
}
func (e *KiloExecutor) HttpRequest(ctx context.Context, a *coreauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil || req.URL.String() != helps.KiloResponsesURL {
		return nil, errors.New("invalid Kilo request destination")
	}
	if err := e.PrepareRequest(req, a); err != nil {
		return nil, err
	}
	return helps.KiloDo(ctx, a, req)
}
func (e *KiloExecutor) send(ctx context.Context, a *coreauth.Auth, req core.Request, opts core.Options, search *helps.KiloToolSearch) (*http.Response, []byte, error) {
	if core.DownstreamWebsocket(ctx) || core.RequiredUpstreamWebsocket(ctx) || opts.Alt != "" {
		return nil, nil, coreauth.NewRequestScopedError("Kilo operation is not supported", 501)
	}
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	model := thinking.ParseSuffix(req.Model).ModelName
	body := bytes.Clone(req.Payload)
	var originalCallIDs map[string]string
	if opts.SourceFormat == translator.FormatOpenAI {
		chat := ojson.NewDoc(body)
		originalCallIDs = helps.PrepareKiloChatCallIDs(chat)
		body = chat.Bytes()
		var err error
		body, err = helps.TranslateRequestReturningError(ctx, opts.Headers, cfg, opts.SourceFormat, translator.FormatCodex, model, body, true, false)
		if err != nil {
			return nil, nil, err
		}
	} else if opts.SourceFormat != translator.FormatOpenAIResponse && opts.SourceFormat != translator.FormatCodex {
		return nil, nil, coreauth.NewRequestScopedError("Kilo supports Responses and Chat Completions", 400)
	}
	doc := ojson.NewDoc(body)
	if opts.SourceFormat == translator.FormatOpenAI {
		helps.PreserveKiloChatFields(doc, req.Payload, originalCallIDs)
		if err := helps.PreserveKiloChatToolResults(ctx, doc, req.Payload); err != nil {
			return nil, nil, err
		}
	}
	if err := search.Prepare(doc); err != nil {
		return nil, nil, coreauth.NewRequestScopedError(err.Error(), 400)
	}
	localCalls, errAdapt := helps.AdaptKiloAgentMessagesWithCalls(doc)
	if errAdapt != nil {
		return nil, nil, coreauth.NewRequestScopedError(errAdapt.Error(), 400)
	}
	if err := helps.ApplyRequestThinkingDoc(doc, req, opts, opts.SourceFormat.String(), translator.FormatCodex.String(), "kilo"); err != nil {
		return nil, nil, err
	}
	_ = doc.Set("model", "openai/"+strings.TrimPrefix(model, "openai/"))
	_ = doc.Set("stream", true)
	original := opts.OriginalRequest
	if len(original) == 0 {
		original = req.Payload
	}
	if !helps.KiloPayloadHasConditionalRules(cfg, model, req, opts) {
		helps.ApplyKiloPayloadCandidates(ctx, doc)
	}
	helps.NewPayloadFinalizer(cfg, "kilo", model, translator.FormatCodex.String(), "", original, req, opts, a).FinalizeDoc(doc)
	body = doc.Bytes()
	if err := helps.ValidateKiloAlphaPayload(ctx, doc, model); err != nil {
		return nil, nil, err
	}
	if err := helps.ValidateKiloPayloadBudget(ctx, body); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := helps.ValidateKiloFinalHistory(ctx, body, localCalls, search); err != nil {
		return nil, nil, err
	}
	if !ojson.GetBytes(body, "stream").Bool() {
		return nil, nil, coreauth.NewRequestScopedError("Kilo requires upstream stream=true", 400)
	}
	if coreauth.KiloDiagnostic(ctx) && (doc.Get("model").String() != "openai/"+req.Model || doc.Get("input").Type != ojson.String || strings.TrimSpace(doc.Get("input").String()) == "") {
		return nil, nil, coreauth.NewRequestScopedError("invalid Kilo diagnostic payload", 400)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, helps.KiloResponsesURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	if err = e.PrepareRequest(httpReq, a); err != nil {
		return nil, nil, err
	}
	if value := opts.Headers.Get("X-Codex-Turn-State"); value != "" {
		httpReq.Header.Set("X-Codex-Turn-State", value)
	}
	if err = coreauth.RecordKiloDiagnosticCall(ctx); err != nil {
		return nil, nil, err
	}
	response, err := helps.KiloDo(ctx, a, httpReq)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		return nil, nil, helps.KiloHTTPResponseError(response.StatusCode, errorBody)
	}
	return response, body, nil
}
func (e *KiloExecutor) Execute(ctx context.Context, a *coreauth.Auth, req core.Request, opts core.Options) (result core.Response, err error) {
	if err = coreauth.BeginKiloDiagnostic(ctx, a); err != nil {
		return result, err
	}
	if coreauth.KiloDiagnosticAutoFree(ctx) {
		return e.executeAutoFreeDiagnostic(ctx, a, req, opts)
	}
	reporter := helps.NewExecutorUsageReporter(ctx, e, req.Model, a)
	defer reporter.TrackFailure(ctx, &err)
	search := &helps.KiloToolSearch{}
	response, body, err := e.send(ctx, a, req, opts, search)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	var terminal []byte
	err = helps.ReadKiloPublicEvents(ctx, response, search, func(event []byte) error { return helps.ObserveKiloBinding(ctx, event) }, func(_ []byte, event []byte) error {
		if ojson.GetBytes(event, "type").String() == "response.completed" {
			_, terminal = helps.KiloPublicEvent(nil, bytes.Clone(event))
		}
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var responseErr *helps.KiloResponseError
		if errors.As(err, &responseErr) {
			return result, err
		}
		return result, statusErr{code: 502, msg: err.Error()}
	}
	if detail, ok := helps.ParseCodexUsage(terminal); ok {
		reporter.Publish(ctx, detail)
	}
	if err := helps.ValidateKiloAlphaTerminal(ctx, terminal); err != nil {
		return result, err
	}
	var parameter any
	payload := translator.TranslateNonStream(ctx, translator.FormatCodex, core.ResponseFormatOrSource(opts), req.Model, opts.OriginalRequest, body, terminal, &parameter)
	if len(payload) == 0 {
		return result, statusErr{code: 502, msg: "Kilo response translation failed"}
	}
	return core.Response{Payload: payload}, nil
}
func (e *KiloExecutor) ExecuteStream(ctx context.Context, a *coreauth.Auth, req core.Request, opts core.Options) (*core.StreamResult, error) {
	reporter := helps.NewExecutorUsageReporter(ctx, e, req.Model, a)
	reporter.SetStream(true)
	search := &helps.KiloToolSearch{}
	response, body, err := e.send(ctx, a, req, opts, search)
	if err != nil {
		reporter.PublishFailure(ctx, err)
		return nil, err
	}
	chunks := make(chan core.StreamChunk)
	go func() {
		defer close(chunks)
		defer response.Body.Close()
		var parameter any
		emit := func(payload []byte) error {
			select {
			case chunks <- core.StreamChunk{Payload: payload}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err := helps.ReadKiloPublicEvents(ctx, response, search, func(event []byte) error { return helps.ObserveKiloBinding(ctx, event) }, func(frame, event []byte) error {
			frame, event = helps.KiloPublicEvent(frame, event)
			if detail, ok := helps.ParseCodexUsage(event); ok {
				reporter.Publish(ctx, detail)
			}
			if core.ResponseFormatOrSource(opts) == translator.FormatOpenAIResponse || core.ResponseFormatOrSource(opts) == translator.FormatCodex {
				return emit(bytes.Clone(frame))
			}
			if len(event) == 0 {
				return nil
			}
			for _, payload := range translator.TranslateStream(ctx, translator.FormatCodex, core.ResponseFormatOrSource(opts), req.Model, opts.OriginalRequest, body, append([]byte("data: "), event...), &parameter) {
				if err := emit(payload); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			reporter.PublishFailure(ctx, err)
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				var responseErr *helps.KiloResponseError
				if !errors.As(err, &responseErr) {
					err = statusErr{code: 502, msg: err.Error()}
				}
			}
			select {
			case chunks <- core.StreamChunk{Err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return &core.StreamResult{Chunks: chunks}, nil
}

// observationCommit serializes background persistence after the runtime health lock is released.
func (e *KiloExecutor) observationCommit(source *helps.KiloRuntime) func(string, string, uint64) {
	return func(id, raw string, generation uint64) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.runtime != source || e.initErr != nil {
			return
		}
		_ = source.CommitProxyObservation(id, raw, generation)
		e.notifyLocked()
	}
}

// The Manager wires this under its existing admission gate.
func (e *KiloExecutor) SetKiloDeviceProfileStore(store kilo.DeviceProfileStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.profileStore = store
	if e.runtime != nil {
		e.runtime.SetDeviceProfileStore(store)
	}
}
