package auth

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type kiloDiagnosticKey struct{}
type kiloDiagnostic struct {
	manager     *Manager
	identity    kilobinding.Identity
	credential  uint64
	epoch       uint64
	restriction uint64
	disabled    bool
	manual      bool
	model       string
	started     atomic.Bool
	probe       *kiloProbeRun
}

func kiloDiagnosticFrom(ctx context.Context) *kiloDiagnostic {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(kiloDiagnosticKey{}).(*kiloDiagnostic)
	return d
}

func KiloDiagnostic(ctx context.Context) bool { return kiloDiagnosticFrom(ctx) != nil }

func staleKiloDiagnostic(ctx context.Context, a *Auth) bool {
	d := kiloDiagnosticFrom(ctx)
	return d != nil && ((d.probe != nil && !d.probe.valid.Load()) || a == nil || a.Provider != "kilo" || KiloIdentity(a) != d.identity || a.CredentialVersion != d.credential || a.RegistrationEpoch != d.epoch || a.Disabled != d.disabled || kiloRestriction(a).Generation != d.restriction)
}

// KiloManualDiagnostic can only originate from ExecuteKiloDiagnostic, never
// from client headers, request metadata, or a public inference payload.
func KiloManualDiagnostic(ctx context.Context, a *Auth) bool {
	d := kiloDiagnosticFrom(ctx)
	return d != nil && d.manual && !staleKiloDiagnostic(ctx, a)
}

func KiloDiagnosticAutoFree(ctx context.Context) bool {
	d := kiloDiagnosticFrom(ctx)
	return d != nil && d.model == "kilo-auto/free"
}

// BeginKiloDiagnostic fences a queued execution against current identity and
// explicit account changes immediately before the existing executor sends it.
func BeginKiloDiagnostic(ctx context.Context, a *Auth) error {
	d := kiloDiagnosticFrom(ctx)
	if d == nil {
		return nil
	}
	current, _ := d.manager.GetByID(d.identity.AuthID)
	if staleKiloDiagnostic(ctx, current) || staleKiloDiagnostic(ctx, a) {
		return ErrKiloIdentityChanged
	}
	d.started.Store(true)
	return nil
}

type KiloDiagnosticResult struct {
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	DurationMS  int64     `json:"duration_ms"`
	Text        string    `json:"text"`
	ErrorCode   *string   `json:"error_code"`
}

// ExecuteKiloDiagnostic reuses normal binding, joint admission, execution and
// result registration. The manual exemption is scoped to this one account.
func (m *Manager) ExecuteKiloDiagnostic(ctx context.Context, id, model, prompt string, manual bool) (KiloDiagnosticResult, error) {
	result := KiloDiagnosticResult{Model: model, Status: "failed", StartedAt: time.Now().UTC()}
	a, exists := m.GetByID(id)
	if !exists || a.Provider != "kilo" {
		return result, ErrKiloAccountMissing
	}
	executor, exists := m.Executor("kilo")
	models, supported := executor.(interface{ KiloDiagnosticModelAllowed(string) bool })
	if !exists || !supported || m.HomeEnabled() {
		return result, NewRequestScopedError("Kilo diagnostic executor is unavailable", 503)
	}
	if strings.TrimSpace(prompt) == "" || !models.KiloDiagnosticModelAllowed(model) {
		return result, NewRequestScopedError("invalid Kilo test model or prompt", 400)
	}
	d := &kiloDiagnostic{manager: m, identity: KiloIdentity(a), credential: a.CredentialVersion, epoch: a.RegistrationEpoch, restriction: kiloRestriction(a).Generation, disabled: a.Disabled, manual: manual, model: model}
	d.probe, _ = ctx.Value(kiloProbeRunKey{}).(*kiloProbeRun)
	ctx = context.WithValue(channelscope.With(ctx, "kilo"), kiloDiagnosticKey{}, d)
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("model", model)
	_ = doc.Set("input", prompt)
	_ = doc.Set("stream", false)
	opts := core.Options{SourceFormat: translator.FormatOpenAIResponse, OriginalRequest: doc.Bytes(), Metadata: map[string]any{core.PinnedAuthMetadataKey: id, core.CallerScopeMetadataKey: "kilo-management-diagnostic"}}
	response, err := m.Execute(ctx, []string{"kilo"}, core.Request{Model: model, Payload: doc.Bytes()}, opts)
	result.CompletedAt = time.Now().UTC()
	result.DurationMS = result.CompletedAt.Sub(result.StartedAt).Milliseconds()
	if err != nil && !d.started.Load() {
		return result, err
	}
	if err == nil {
		result.Status = "completed"
		for _, item := range ojson.GetBytes(response.Payload, "output").Array() {
			for _, content := range item.Get("content").Array() {
				if content.Get("type").String() == "output_text" {
					result.Text += content.Get("text").String()
				}
			}
		}
		return result, nil
	}
	code := "request_failed"
	var incomplete interface{ DiagnosticIncompleteText() string }
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		result.Status, code = "canceled", "canceled"
	} else if errors.As(err, &incomplete) {
		result.Status, code, result.Text = "incomplete", "incomplete", incomplete.DiagnosticIncompleteText()
	}
	result.ErrorCode = &code
	return result, nil
}

// Called under the existing selection mutex; the shared caller still owns all
// account-capacity, egress acquisition, wait-budget and release logic.
func (m *Manager) pickKiloDiagnostic(ctx context.Context, tried map[string]struct{}) (*Auth, ProviderExecutor, string, error) {
	d := kiloDiagnosticFrom(ctx)
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.auths[d.identity.AuthID]
	if staleKiloDiagnostic(ctx, a) {
		return nil, nil, "", ErrKiloIdentityChanged
	}
	if _, excluded := tried[a.ID]; excluded {
		return nil, nil, "", NewRequestScopedError("Kilo diagnostic account is busy", 409)
	}
	if !d.manual {
		if blocked, _, _ := isAuthBlockedForModelForRequest(ctx, a, d.model, time.Now()); blocked {
			return nil, nil, "", NewRequestScopedError("Kilo diagnostic account is unavailable", 409)
		}
	}
	executor, ok := m.executorLocked("kilo")
	if !ok {
		return nil, nil, "", NewRequestScopedError("Kilo diagnostic executor is unavailable", 503)
	}
	return a.Clone(), executor, "kilo", nil
}
