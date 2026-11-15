package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
)

const kiloProbeConfigKey = "kilo_probe_config"

var ErrKiloProbeConflict = errors.New("Kilo probe configuration revision conflict")
var ErrKiloProbeInvalid = errors.New("invalid Kilo probe configuration")

// KiloProbeConfig is persisted in the existing credential metadata only.
type KiloProbeConfig struct {
	Revision        uint64
	HealthEnabled   bool
	ModelEnabled    bool
	Model           string
	IntervalSeconds int
}

func kiloProbeConfig(a *Auth) KiloProbeConfig {
	c := KiloProbeConfig{IntervalSeconds: 300}
	if a == nil {
		return c
	}
	v, _ := a.Metadata[kiloProbeConfigKey].(map[string]any)
	c.Revision = kiloUint(v["revision"])
	c.HealthEnabled, _ = v["health_enabled"].(bool)
	c.ModelEnabled, _ = v["model_enabled"].(bool)
	c.Model, _ = v["model"].(string)
	if interval := int(kiloUint(v["interval_seconds"])); interval >= 60 && interval <= 86400 {
		c.IntervalSeconds = interval
	}
	return c
}

func applyKiloProbeConfig(a *Auth, c KiloProbeConfig) {
	a.Metadata[kiloProbeConfigKey] = map[string]any{"revision": c.Revision, "health_enabled": c.HealthEnabled, "model_enabled": c.ModelEnabled, "model": c.Model, "interval_seconds": c.IntervalSeconds}
}

func (m *Manager) validateKiloProbeConfig(c KiloProbeConfig) error {
	if c.IntervalSeconds < 60 || c.IntervalSeconds > 86400 || (c.ModelEnabled && c.Model == "") {
		return ErrKiloProbeInvalid
	}
	if c.Model == "" {
		return nil
	}
	e, ok := m.Executor("kilo")
	models, supported := e.(interface{ KiloDiagnosticModelAllowed(string) bool })
	if !ok || !supported || !models.KiloDiagnosticModelAllowed(c.Model) {
		return ErrKiloProbeInvalid
	}
	return nil
}

type KiloProbeKindView struct {
	Enabled         bool       `json:"enabled"`
	Model           string     `json:"model"`
	IntervalSeconds int        `json:"interval_seconds"`
	Status          string     `json:"status"`
	NextRunAt       *time.Time `json:"next_run_at"`
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastCompletedAt *time.Time `json:"last_completed_at"`
	ErrorCode       *string    `json:"error_code"`
	ModelCalls      uint64     `json:"model_calls"`
	SkippedCount    uint64     `json:"skipped_count"`
}

type KiloProbesView struct {
	AccountID        string            `json:"account_id"`
	Revision         uint64            `json:"revision"`
	RuntimeStartedAt time.Time         `json:"runtime_started_at"`
	ManualModelCalls uint64            `json:"manual_model_calls"`
	Health           KiloProbeKindView `json:"health"`
	ModelProbe       KiloProbeKindView `json:"model_probe"`
}

type kiloProbeFence struct {
	identity          kilobinding.Identity
	credential, epoch uint64
	disabled          bool
	proxy             string
	direct            bool
	config            KiloProbeConfig
}

func kiloProbeIdentity(a *Auth) kiloProbeFence {
	direct, _ := a.Metadata["allow_direct_when_proxy_full"].(bool)
	return kiloProbeFence{KiloIdentity(a), a.CredentialVersion, a.RegistrationEpoch, a.Disabled, a.ProxyURL, direct, kiloProbeConfig(a)}
}

type kiloProbeRuntime struct {
	fence    kiloProbeFence
	view     KiloProbesView
	active   *kiloProbeRun
	lastKind int
}

type kiloProbeRun struct {
	cancel      context.CancelFunc
	done        chan struct{}
	valid       atomic.Bool
	kind        int
	restriction uint64
	runtime     *kiloProbeRuntime
}

type kiloProbeRunKey struct{}
type kiloProbeLifecycle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *kiloProbeRuntime) kind(k int) *KiloProbeKindView {
	if k == 0 {
		return &r.view.Health
	}
	return &r.view.ModelProbe
}

func (m *Manager) kiloProbeRuntimeLocked(a *Auth, now time.Time) *kiloProbeRuntime {
	if m.kiloProbeStarted.IsZero() {
		m.kiloProbeStarted = now.UTC()
	}
	if m.kiloProbeStates == nil {
		m.kiloProbeStates = map[string]*kiloProbeRuntime{}
	}
	r := m.kiloProbeStates[a.ID]
	if r == nil {
		r = &kiloProbeRuntime{fence: kiloProbeIdentity(a), lastKind: 1}
		r.view = KiloProbesView{AccountID: a.ID, RuntimeStartedAt: m.kiloProbeStarted, Health: KiloProbeKindView{Status: "never"}, ModelProbe: KiloProbeKindView{Status: "never"}}
		m.kiloProbeStates[a.ID] = r
		m.scheduleKiloProbesLocked(r, now)
	}
	return r
}

func (m *Manager) scheduleKiloProbesLocked(r *kiloProbeRuntime, now time.Time) {
	c := r.fence.config
	r.view.Revision = c.Revision
	r.view.Health.Enabled, r.view.Health.Model, r.view.Health.IntervalSeconds = c.HealthEnabled, "kilo-auto/free", 300
	r.view.ModelProbe.Enabled, r.view.ModelProbe.Model, r.view.ModelProbe.IntervalSeconds = c.ModelEnabled, c.Model, c.IntervalSeconds
	for k := range 2 {
		v := r.kind(k)
		v.NextRunAt = nil
		if v.Enabled && !m.kiloProbeStopped {
			at := now.Add(time.Duration(v.IntervalSeconds) * time.Second).UTC()
			v.NextRunAt = &at
			if v.Status == "never" {
				v.Status = "scheduled"
			}
		}
	}
}

func cancelKiloProbeLocked(r *kiloProbeRuntime, now time.Time) {
	if run := r.active; run != nil && run.valid.Swap(false) {
		run.cancel()
		v := r.kind(run.kind)
		v.Status, v.ErrorCode, v.LastCompletedAt = "canceled", kiloProbeCode("canceled"), kiloProbeTime(now)
	}
}

// Structural notifications deliberately exclude ordinary result generations.
func (m *Manager) invalidateKiloProbeStateLocked(id string) {
	r := m.kiloProbeStates[id]
	a := m.auths[id]
	if r == nil {
		if a != nil && a.Provider == "kilo" {
			m.kiloProbeRuntimeLocked(a, time.Now())
		}
		return
	}
	if a == nil || a.Provider != "kilo" {
		cancelKiloProbeLocked(r, time.Now())
		if r.active == nil {
			delete(m.kiloProbeStates, id)
		}
		return
	}
	fence := kiloProbeIdentity(a)
	if r.fence == fence {
		return
	}
	cancelKiloProbeLocked(r, time.Now())
	if r.fence.config.Model != fence.config.Model {
		v := &r.view.ModelProbe
		v.Status, v.ErrorCode = "never", nil
		v.LastStartedAt, v.LastCompletedAt = nil, nil
	}
	r.fence = fence
	m.scheduleKiloProbesLocked(r, time.Now())
}

func (m *Manager) invalidateKiloProbeEgress(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for accountID, r := range m.kiloProbeStates {
		if id == "" || id == accountID {
			cancelKiloProbeLocked(r, time.Now())
			m.scheduleKiloProbesLocked(r, time.Now())
		}
	}
}

func (m *Manager) KiloProbes(id string) (KiloProbesView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.auths[id]
	if a == nil || a.Provider != "kilo" {
		return KiloProbesView{}, ErrKiloAccountMissing
	}
	return m.kiloProbeRuntimeLocked(a, time.Now()).view, nil
}

func kiloProbeCode(code string) *string     { return &code }
func kiloProbeTime(at time.Time) *time.Time { at = at.UTC(); return &at }

func kiloProbeUnavailable(ctx context.Context, a *Auth, model string, now time.Time) string {
	if a.Disabled {
		return "account_disabled"
	}
	if kiloAutoBlocked(a) {
		return "account_restricted"
	}
	if blocked, _, _ := isAuthBlockedForModelForRequest(ctx, a, model, now); blocked {
		return "account_unavailable"
	}
	return ""
}

// ProcessKiloProbes is a deterministic due-cycle seam. Each cycle starts at most
// one task per account; the returned signal covers only tasks started by it.
func (m *Manager) ProcessKiloProbes(ctx context.Context, now time.Time) <-chan struct{} {
	done := make(chan struct{})
	var wg sync.WaitGroup
	m.mu.Lock()
	if ctx.Err() == nil && !m.kiloProbeStopped {
		for _, a := range m.auths {
			if a.Provider != "kilo" {
				continue
			}
			r := m.kiloProbeRuntimeLocked(a, now)
			due := [2]bool{}
			for k := range 2 {
				v := r.kind(k)
				due[k] = v.Enabled && v.NextRunAt != nil && !v.NextRunAt.After(now)
				if due[k] {
					v.NextRunAt = kiloProbeTime(now.Add(time.Duration(v.IntervalSeconds) * time.Second))
				}
			}
			selected := -1
			for _, k := range []int{1 - r.lastKind, r.lastKind} {
				if !due[k] {
					continue
				}
				v := r.kind(k)
				code := kiloProbeUnavailable(ctx, a, v.Model, now)
				if code == "" && (r.active != nil || selected >= 0) {
					code = "account_busy"
				}
				if code != "" {
					v.SkippedCount++
					if r.active == nil || r.active.kind != k || !r.active.valid.Load() {
						v.Status, v.ErrorCode, v.LastCompletedAt = "skipped", kiloProbeCode(code), kiloProbeTime(now)
					}
					continue
				}
				selected = k
			}
			if selected < 0 {
				continue
			}
			runCtx, cancel := context.WithCancel(ctx)
			run := &kiloProbeRun{cancel: cancel, done: make(chan struct{}), kind: selected, runtime: r, restriction: kiloRestriction(a).Generation}
			run.valid.Store(true)
			r.active, r.lastKind = run, selected
			v := r.kind(selected)
			v.Status, v.ErrorCode, v.LastStartedAt = "running", nil, kiloProbeTime(now)
			wg.Add(1)
			go func(id, model string) {
				defer wg.Done()
				m.runKiloProbe(context.WithValue(runCtx, kiloProbeRunKey{}, run), id, model, run)
			}(a.ID, v.Model)
		}
	}
	m.mu.Unlock()
	go func() { wg.Wait(); close(done) }()
	return done
}

func (m *Manager) runKiloProbe(ctx context.Context, id, model string, run *kiloProbeRun) {
	defer close(run.done)
	defer run.cancel()
	result, err := m.ExecuteKiloDiagnostic(ctx, id, model, "hi", false)
	m.mu.Lock()
	defer m.mu.Unlock()
	r := run.runtime
	if r.active != run {
		return
	}
	r.active = nil
	a := m.auths[id]
	if a == nil && m.kiloProbeStates[id] == r {
		delete(m.kiloProbeStates, id)
	}
	if !run.valid.Load() || m.kiloProbeStates[id] != r {
		return
	}
	v := r.kind(run.kind)
	v.LastCompletedAt = kiloProbeTime(time.Now())
	if ctx.Err() != nil || a == nil || kiloProbeIdentity(a) != r.fence || (result.Status == "completed" && kiloRestriction(a).Generation != run.restriction) {
		v.Status, v.ErrorCode = "canceled", kiloProbeCode("canceled")
	} else if err != nil {
		code := kiloProbeUnavailable(ctx, a, model, time.Now())
		if code != "" {
			v.Status, v.ErrorCode = "skipped", kiloProbeCode(code)
			v.SkippedCount++
		} else {
			v.Status, v.ErrorCode = "failed", kiloProbeCode("request_failed")
		}
	} else {
		v.Status, v.ErrorCode = result.Status, result.ErrorCode
	}
}

// RecordKiloDiagnosticCall is the final read-only fence before model transport.
// Payload rejection, quota reads and proxy checks never enter this counter.
func RecordKiloDiagnosticCall(ctx context.Context) error {
	d := kiloDiagnosticFrom(ctx)
	if d == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m := d.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.auths[d.identity.AuthID]
	if staleKiloDiagnostic(ctx, a) {
		return ErrKiloIdentityChanged
	}
	r := m.kiloProbeRuntimeLocked(a, time.Now())
	if d.manual {
		r.view.ManualModelCalls++
	} else if d.probe != nil && d.probe.runtime == r {
		r.kind(d.probe.kind).ModelCalls++
	}
	return nil
}

func (m *Manager) StartKiloProbes(parent context.Context) {
	ticker := time.NewTicker(time.Second)
	m.startKiloProbes(parent, ticker.C, ticker.Stop)
}

func (m *Manager) startKiloProbes(parent context.Context, ticks <-chan time.Time, stop func()) {
	m.kiloProbeLifecycleMu.Lock()
	defer m.kiloProbeLifecycleMu.Unlock()
	m.stopKiloProbes()
	ctx, cancel := context.WithCancel(parent)
	loop := &kiloProbeLifecycle{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	m.kiloProbeStates = map[string]*kiloProbeRuntime{}
	m.kiloProbeStarted, m.kiloProbeStopped, m.kiloProbeLoop = time.Now().UTC(), false, loop
	for _, a := range m.auths {
		if a.Provider == "kilo" {
			m.kiloProbeRuntimeLocked(a, m.kiloProbeStarted)
		}
	}
	m.mu.Unlock()
	go func() {
		defer close(loop.done)
		defer stop()
		defer func() {
			m.mu.Lock()
			if m.kiloProbeLoop == loop {
				m.kiloProbeStopped = true
				m.cancelKiloProbeTasksLocked()
			}
			m.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticks:
				m.ProcessKiloProbes(ctx, now)
			}
		}
	}()
}

func (m *Manager) cancelKiloProbeTasksLocked() []<-chan struct{} {
	var tasks []<-chan struct{}
	for _, r := range m.kiloProbeStates {
		cancelKiloProbeLocked(r, time.Now())
		r.view.Health.NextRunAt, r.view.ModelProbe.NextRunAt = nil, nil
		if r.active != nil {
			tasks = append(tasks, r.active.done)
		}
	}
	return tasks
}

func (m *Manager) stopKiloProbes() {
	m.mu.Lock()
	loop := m.kiloProbeLoop
	m.kiloProbeLoop, m.kiloProbeStopped = nil, true
	tasks := m.cancelKiloProbeTasksLocked()
	m.mu.Unlock()
	if loop != nil {
		loop.cancel()
		<-loop.done
	}
	for _, done := range tasks {
		<-done
	}
}

func (m *Manager) StopKiloProbes() {
	m.kiloProbeLifecycleMu.Lock()
	defer m.kiloProbeLifecycleMu.Unlock()
	m.stopKiloProbes()
}
