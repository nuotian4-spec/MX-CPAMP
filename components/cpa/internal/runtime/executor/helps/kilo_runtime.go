package helps

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

const KiloResponsesURL = "https://ai-gateway.kilo.ai/api/v1/responses"

var KiloModels = []string{"gpt-5.6-sol", "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna", "gpt-5.6-luna", "gpt-6.2-sol", "gpt-6.2-astra"}

type KiloRuntime struct {
	profileStore      kilo.DeviceProfileStore
	profileContext    context.Context
	profileError      error
	autoAccounts      map[string]kilo.EgressAccountStatus
	autoError         *string
	mu                sync.Mutex
	config            config.KiloConfig
	pool              *kilo.ProxyPool
	urls              map[string]string
	clients           map[string]*http.Client
	health            map[string]kiloProxyHealth
	wake              func()
	observationCommit func(string, string, uint64)
	state             kilo.ProxyState
	managed           bool
	store             kilo.ProxyStateStore
	probeSequence     uint64
	devicePins        map[string]*kiloDevicePin
}

type kiloProxyHealth struct {
	admissionReason string
	observedIP      string
	observedAt      time.Time
	checked         bool
	pending         bool
	healthy         bool
	checkedAt       time.Time
	generation      uint64
}

type kiloLease struct {
	profile kilo.DeviceProfile
	runtime *KiloRuntime
	route   kilo.Route
	client  *http.Client
}

func NewKiloRuntime(cfg config.KiloConfig) (*KiloRuntime, error) {
	cfg.Proxies = append([]config.KiloProxy(nil), cfg.Proxies...)
	proxies := make([]kilo.Proxy, 0, len(cfg.Proxies))
	urls := make(map[string]string, len(cfg.Proxies))
	for _, proxy := range cfg.Proxies {
		setting, err := proxyutil.Parse(proxy.URL)
		if err != nil || setting.Mode != proxyutil.ModeProxy {
			return nil, errors.New("Kilo proxy configuration is invalid")
		}
		urls[proxy.ID] = proxy.URL
		proxies = append(proxies, kilo.Proxy{ID: proxy.ID, AccountID: proxy.AccountID, Healthy: false})
	}
	pool, err := kilo.NewProxyPool(proxies, cfg.ProxyLimit)
	if err != nil {
		return nil, errors.New("Kilo proxy pool configuration is invalid")
	}
	return &KiloRuntime{config: cfg, pool: pool, urls: urls, clients: map[string]*http.Client{}, health: map[string]kiloProxyHealth{}}, nil
}

// InheritHealth preserves observations for unchanged destinations; configuration
// edits are not evidence that a previously failed proxy recovered.
func (r *KiloRuntime) InheritHealth(previous *KiloRuntime) {
	previous.mu.Lock()
	defer previous.mu.Unlock()
	r.devicePins = previous.devicePins
	for _, pin := range r.devicePins {
		if pin.authorization.Mode == "pool" && r.urls[pin.authorization.ProxyID] != previous.urls[pin.authorization.ProxyID] {
			pin.cancelled = true
			if pin.cancel != nil {
				pin.cancel()
			}
		}
	}
	r.cancelDisabledPins(r.state)
	for _, proxy := range r.config.Proxies {
		if r.urls[proxy.ID] != previous.urls[proxy.ID] {
			continue
		}
		state := previous.health[proxy.ID]
		if state.pending {
			continue
		}
		r.health[proxy.ID] = state

	}
	r.refreshPoolHealthLocked()
}
func (r *KiloRuntime) SetWake(wake func()) { r.wake = wake }
func (r *KiloRuntime) SetObservationCommit(commit func(string, string, uint64)) {
	r.observationCommit = commit
}

// Probe only proxy connectivity. No account credential or model request is sent.
// As in the source allocator, unverified destinations do not count as capacity.
func (r *KiloRuntime) prepareHealth(accountID string, proxyIDs ...string) bool {
	return r.prepareProxyHealth(accountID, true, proxyIDs...)
}

func (r *KiloRuntime) prepareProxyHealth(accountID string, refreshExpired bool, proxyIDs ...string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending := false
	for _, proxy := range r.config.Proxies {
		pendingAdmission := false
		for _, p := range r.state.Proxies {
			if p.ID == proxy.ID && p.AdmissionState() == "pending" {
				pendingAdmission = true
				break
			}
		}
		if pendingAdmission {
			continue
		}
		if proxy.Disabled || (proxy.AccountID != "" && proxy.AccountID != accountID) {
			continue
		}
		if r.state.Accounts[accountID].Mode == "auto" {
			owned := false
			for _, p := range r.state.Proxies {
				if p.ID == proxy.ID && p.FixedIP != "" && r.state.IPGroups[p.FixedIP].Owner == accountID {
					owned = true
				}
			}
			if !owned {
				continue
			}
		}
		if len(proxyIDs) > 0 && proxy.ID != proxyIDs[0] {
			continue
		}
		if candidates := r.state.Accounts[accountID].CandidateProxyIDs; candidates != nil && !slices.Contains(candidates, proxy.ID) {
			continue
		}
		state := r.health[proxy.ID]
		if refreshExpired && state.checked && state.healthy && time.Since(state.checkedAt) >= time.Minute {
			state.checked = false
			state.healthy = false
			_ = r.pool.SetHealthy(proxy.ID, false)
		}
		if !state.checked && !state.pending {
			state.pending = true
			r.probeSequence++
			state.generation = r.probeSequence
			r.health[proxy.ID] = state
			go r.probe(proxy.ID, proxy.URL, state.generation)
		}
		pending = pending || state.pending
	}
	return pending
}

// ProbeKiloProxy always creates a fresh transport and bypasses cached observations.
func ProbeKiloProxy(ctx context.Context, raw string) error {
	_, err := ProbeKiloProxyObservation(ctx, raw)
	return err
}

func ProbeKiloProxyObservation(ctx context.Context, raw string) (string, error) {
	transport, mode, err := proxyutil.BuildHTTPTransport(raw)
	if err != nil || mode != proxyutil.ModeProxy || transport == nil {
		return "", kilo.ErrProxyHealth
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org/", nil)
	req.Header.Set("Accept", "text/plain")
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", kilo.ErrProxyHealth
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 129))
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if response.StatusCode != http.StatusOK || err != nil || len(body) > 128 || net.ParseIP(strings.TrimSpace(string(body))) == nil {
		return "", kilo.ErrProxyHealth
	}
	rawIP := strings.TrimSpace(string(body))
	if ip, ok := kilo.FixedPublicIP(rawIP); ok {
		return ip, nil
	}
	return rawIP, nil
}
func (r *KiloRuntime) probe(id, raw string, generation uint64) {
	observedIP, probeErr := ProbeKiloProxyObservation(context.Background(), raw)
	healthy := probeErr == nil

	r.mu.Lock()
	if r.urls[id] != raw || r.health[id].generation != generation {
		r.mu.Unlock()
		return
	}
	observation := r.health[id]
	observation.checked, observation.pending, observation.healthy, observation.checkedAt, observation.generation = true, false, healthy, time.Now(), generation
	if healthy {
		observation.observedIP, observation.observedAt = observedIP, observation.checkedAt
	}
	r.quarantineObservationLocked(id, &observation)
	r.health[id] = observation
	r.refreshPoolHealthLocked()
	r.mu.Unlock()
	if r.observationCommit != nil {
		r.observationCommit(id, raw, generation)
	}
	if r.wake != nil {
		r.wake()
	}
}
func (r *KiloRuntime) markFailed(id string) {
	r.mu.Lock()
	r.probeSequence++
	observation := r.health[id]
	observation.checked, observation.pending, observation.healthy, observation.checkedAt, observation.generation = true, false, false, time.Now(), r.probeSequence
	r.quarantineObservationLocked(id, &observation)
	r.health[id] = observation
	raw := r.urls[id]
	generation := observation.generation
	_ = r.pool.SetHealthy(id, false)
	r.mu.Unlock()
	if r.observationCommit != nil {
		r.observationCommit(id, raw, generation)
	}
	if r.wake != nil {
		r.wake()
	}
}

func (r *KiloRuntime) Matches(cfg config.KiloConfig) bool {
	previous := r.config
	previous.ImageReductionEnabled = false
	cfg.ImageReductionEnabled = false
	return reflect.DeepEqual(previous, cfg)
}
func (r *KiloRuntime) Changes() <-chan struct{}    { return r.pool.Changes() }
func (r *KiloRuntime) Snapshot() kilo.PoolSnapshot { return r.pool.Snapshot() }
func (r *KiloRuntime) CloseIdleConnections() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, client := range r.clients {
		client.CloseIdleConnections()
	}
}

func (r *KiloRuntime) TryAcquire(auth *coreauth.Auth) (*coreauth.Auth, func(), bool, error) {
	required := ""
	r.profileError = nil
	if selection, ok := auth.Runtime.(*coreauth.KiloDeviceSelection); ok {
		r.profileContext = selection.Context
		required = selection.RequiredProfileID
	}
	defer func() { r.profileContext = nil }()
	next := r.state.Clone()
	changed, profileErr := r.prepareDeviceProfiles(&next, auth.ID, "")
	if profileErr != nil {
		return nil, nil, false, deviceProfileError(profileErr)
	}
	if changed {
		if _, profileErr = r.commitState(next); profileErr != nil {
			return nil, nil, false, deviceProfileError(profileErr)
		}
	}
	egress, configured := r.state.Accounts[auth.ID]
	automatic := egress.Mode == "auto" || (!configured && r.state.AutoAssignmentEnabled && !auth.CreatedAt.Before(r.state.AutoEnabledAt))
	if automatic {
		r.prepareHealth(auth.ID)
	}
	if automatic && !r.ipPolicyReady(auth.ID) {
		r.expandIPCapacity(auth.ID)
		if r.profileError != nil {
			return nil, nil, false, deviceProfileError(r.profileError)
		}
		if !r.ipPolicyReady(auth.ID) {
			return nil, nil, true, nil
		}
	}
	direct := r.state.Accounts[auth.ID].Mode == "direct"
	allow, _ := auth.Metadata["allow_direct_when_proxy_full"].(bool)
	if automatic {
		allow = false
	}
	if required != "" && r.state.Accounts[auth.ID].DeviceProfiles["direct"] == required && !direct {
		if !allow {
			return nil, nil, false, coreauth.NewRequestScopedError("Kilo continuation direct route is disabled", 409)
		}
		direct = true
	}
	if !direct && r.prepareHealth(auth.ID) {
		return nil, nil, true, nil
	}
	var lease *kilo.ProxyLease
	var err error
	if direct {
		if required != "" && r.state.Accounts[auth.ID].DeviceProfiles["direct"] != required {
			return nil, nil, false, coreauth.NewRequestScopedError("Kilo continuation device has no eligible egress", 409)
		}
		lease = kilo.DirectLease()
	} else {
		options := kilo.AcquireOptions{AllowDirectWhenProxyFull: allow}
		if required != "" {
			options.ProxyIDs = r.requiredDeviceProxies(auth, required)
			options.AllowDirectWhenProxyFull = false
			if len(options.ProxyIDs) == 0 {
				return nil, nil, false, coreauth.NewRequestScopedError("Kilo continuation device has no eligible egress", 409)
			}
		}
		if automatic {
			options.Groups = r.state.OwnedIPs(auth.ID)
		}
		lease, err = r.pool.TryAcquire(auth.ID, options)
		if automatic && required == "" && (errors.Is(err, kilo.ErrProxyCapacity) || errors.Is(err, kilo.ErrProxyUnavailable)) && r.expandIPCapacity(auth.ID) {
			options.Groups = r.state.OwnedIPs(auth.ID)
			lease, err = r.pool.TryAcquire(auth.ID, options)
		}
	}
	if r.profileError != nil {
		if lease != nil {
			lease.Release()
		}
		return nil, nil, false, deviceProfileError(r.profileError)
	}
	if errors.Is(err, kilo.ErrProxyCapacity) {
		return nil, nil, true, nil
	}
	if err != nil {
		if required != "" {
			return nil, nil, false, coreauth.NewRequestScopedError("Kilo continuation device has no eligible egress", 409)
		}
		return nil, nil, false, coreauth.NewRequestScopedError("Kilo proxy unavailable", 503)
	}
	route := lease.Route()
	profile, profileErr := r.selectedDeviceProfile(auth, route)
	if profileErr != nil {
		lease.Release()
		return nil, nil, false, profileErr
	}
	raw := "direct"
	if route.Mode == kilo.RouteProxy {
		raw = r.urls[route.ProxyID]
	}
	key := auth.ID + "\x00" + profile.ID + "\x00" + route.ProxyID + "\x00" + string(route.Mode)
	r.mu.Lock()
	client := r.clients[key]
	if client == nil {
		transport, mode, errTransport := proxyutil.BuildHTTPTransport(raw)
		if errTransport != nil || transport == nil || (mode != proxyutil.ModeProxy && mode != proxyutil.ModeDirect) {
			r.mu.Unlock()
			lease.Release()
			return nil, nil, false, &coreauth.Error{Code: "kilo_egress", Message: "Kilo proxy transport unavailable", HTTPStatus: 503}
		}
		transport.ResponseHeaderTimeout = 0
		transport.TLSHandshakeTimeout = 0
		transport.ExpectContinueTimeout = 0
		client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		r.clients[key] = client
	}
	r.mu.Unlock()
	clone := auth.Clone()
	clone.Runtime = &kiloLease{profile: profile, runtime: r, route: route, client: client}
	return clone, lease.Release, false, nil
}

func KiloDo(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	lease, ok := auth.Runtime.(*kiloLease)
	if !ok || lease.client == nil {
		return nil, &coreauth.Error{Code: "kilo_egress", Message: "Kilo request has no admitted egress", HTTPStatus: 503}
	}
	// Disable automatic POST replay on stale pooled connections.
	req.GetBody = nil
	core.MarkUpstreamAttempt(ctx)
	response, err := lease.client.Do(req.WithContext(ctx))
	if err != nil {
		if ctx.Err() != nil {
			// Cancel detached transport dials that no longer have a waiter.
			// Connections serving other requests are not idle and remain intact.
			lease.client.CloseIdleConnections()
			return nil, ctx.Err()
		}
		if lease.route.Mode == kilo.RouteProxy {
			lease.runtime.markFailed(lease.route.ProxyID)
		}
		return nil, &coreauth.Error{Code: coreauth.ErrorCodeConnectionLifecycle, Message: "Kilo upstream connection failed", HTTPStatus: 502}
	}
	return response, nil
}
