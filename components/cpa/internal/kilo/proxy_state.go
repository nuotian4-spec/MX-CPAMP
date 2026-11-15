package kilo

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

var ErrStateStorage = errors.New("Kilo proxy state storage is unavailable")
var ErrStateConflict = errors.New("Kilo proxy state revision conflict")
var ErrStateBusy = errors.New("Kilo egress has active requests")
var ErrStateInvalid = errors.New("invalid Kilo proxy state fields")
var ErrProxyHealth = errors.New("Kilo proxy health check failed")
var ErrProxyMissing = errors.New("Kilo proxy not found")
var ErrProxyIPAlreadyRegistered = errors.New("proxy_ip_already_registered")

type ProxyIPAlreadyRegisteredError struct {
	IP, ProxyID string
}

func (e *ProxyIPAlreadyRegisteredError) Error() string { return ErrProxyIPAlreadyRegistered.Error() }
func (e *ProxyIPAlreadyRegisteredError) Unwrap() error { return ErrProxyIPAlreadyRegistered }

const StateFileName = "kilo-proxy-state.json"

type SavedProxy struct {
	AdmissionStatus, AdmissionReason, AdmittedIP string
	AdmittedAt                                   time.Time
	ID, URL, Name, AccountID                     string
	FixedIP                                      string
	Order                                        int
	Disabled                                     bool
	FirstUsed                                    uint64
}
type ProxyIPRegistration struct {
	IP              string     `json:"ip"`
	ProxyIDs        []string   `json:"proxy_ids"`
	FirstAdmittedAt *time.Time `json:"first_admitted_at"`
	LastObservedAt  *time.Time `json:"last_observed_at"`
	Source          string     `json:"source"`
}

func (p SavedProxy) AdmissionState() string {
	if p.AdmissionStatus == "" {
		return "legacy"
	}
	return p.AdmissionStatus
}

type ProxyAdmissionView struct {
	AdmissionStatus string     `json:"admission_status"`
	AdmissionReason string     `json:"admission_reason"`
	AdmittedIP      string     `json:"admitted_ip"`
	AdmittedAt      *time.Time `json:"admitted_at"`
}

func (p SavedProxy) AdmissionView() ProxyAdmissionView {
	v := ProxyAdmissionView{AdmissionStatus: p.AdmissionState(), AdmissionReason: p.AdmissionReason, AdmittedIP: p.AdmittedIP}
	if !p.AdmittedAt.IsZero() {
		at := p.AdmittedAt.UTC()
		v.AdmittedAt = &at
	}
	return v
}

type ArchivedProxy struct {
	Proxy      SavedProxy
	ArchivedAt time.Time
}
type AccountEgress struct {
	DeviceProfiles map[string]string
	CapacityPolicy
	Mode, PrimaryProxyID string
	CandidateProxyIDs    []string
}
type ProxyState struct {
	IPConcurrencyLimit    int
	IPEntryLimit          *int
	AutoAssignmentEnabled bool
	MaxUniqueAccounts     int
	AutoEnabledAt         time.Time
	IPHistory             map[string]ProxyIPRegistration
	IPGroups              map[string]IPGroup
	Revision              uint64
	Limit                 int
	Proxies               []SavedProxy
	Recycle               []ArchivedProxy
	Accounts              map[string]AccountEgress
	PendingDevices        map[string]PendingDeviceSave
}

func (s ProxyState) Clone() ProxyState {
	groups := map[string]IPGroup{}
	for ip, g := range s.IPGroups {
		g.AccountIDs = slices.Clone(g.AccountIDs)
		groups[ip] = g
	}
	s.IPGroups = groups
	history := make(map[string]ProxyIPRegistration, len(s.IPHistory))
	for ip, entry := range s.IPHistory {
		entry.ProxyIDs = slices.Clone(entry.ProxyIDs)
		if entry.FirstAdmittedAt != nil {
			at := *entry.FirstAdmittedAt
			entry.FirstAdmittedAt = &at
		}
		if entry.LastObservedAt != nil {
			at := *entry.LastObservedAt
			entry.LastObservedAt = &at
		}
		history[ip] = entry
	}
	s.IPHistory = history
	s.Proxies = append([]SavedProxy(nil), s.Proxies...)
	s.Recycle = append([]ArchivedProxy(nil), s.Recycle...)
	accounts := make(map[string]AccountEgress, len(s.Accounts))
	for id, a := range s.Accounts {
		a.CandidateProxyIDs = slices.Clone(a.CandidateProxyIDs)
		a.SpecifiedIPs = slices.Clone(a.SpecifiedIPs)
		a.DeviceProfiles = maps.Clone(a.DeviceProfiles)
		accounts[id] = a
	}
	s.Accounts = accounts
	pending := map[string]PendingDeviceSave{}
	for id, p := range s.PendingDevices {
		pending[id] = p.Clone()
	}
	s.PendingDevices = pending
	return s
}
func (s ProxyState) Validate() error {
	if (s.IPConcurrencyLimit != 0 && validateLimit(s.IPConcurrencyLimit) != nil) || (s.IPEntryLimit != nil && validateLimit(*s.IPEntryLimit) != nil) {
		return ErrStateInvalid
	}
	if s.validateAdmission() != nil {
		return ErrStateInvalid
	}
	if s.validateAuto() != nil {
		return ErrStateInvalid
	}
	if validateLimit(s.Limit) != nil {
		return ErrStateInvalid
	}
	byID := map[string]SavedProxy{}
	for _, p := range s.Proxies {
		setting, err := proxyutil.Parse(p.URL)
		if p.ID == "" || p.Order < 0 || err != nil || setting.Mode != proxyutil.ModeProxy {
			return ErrStateInvalid
		}
		if _, exists := byID[p.ID]; exists {
			return ErrStateInvalid
		}
		byID[p.ID] = p
	}
	archivedIDs := map[string]bool{}
	for _, entry := range s.Recycle {
		p := entry.Proxy
		setting, err := proxyutil.Parse(p.URL)
		if p.ID == "" || p.Order < 0 || entry.ArchivedAt.IsZero() || err != nil || setting.Mode != proxyutil.ModeProxy || archivedIDs[p.ID] {
			return ErrStateInvalid
		}
		if _, exists := byID[p.ID]; exists {
			return ErrStateInvalid
		}
		archivedIDs[p.ID] = true
	}
	for id, a := range s.Accounts {
		if a.CapacityPolicy.Validate(a.Mode) != nil {
			return ErrStateInvalid
		}
		if id == "" || (a.Mode != "pool" && a.Mode != "direct" && a.Mode != "auto") {
			return ErrStateInvalid
		}
		if a.CandidateProxyIDs != nil {
			if len(a.CandidateProxyIDs) == 0 || a.Mode == "direct" {
				return ErrStateInvalid
			}
			seen := map[string]bool{}
			for _, pid := range a.CandidateProxyIDs {
				p, ok := byID[pid]
				if !ok || seen[pid] || (p.AccountID != "" && p.AccountID != id) {
					return ErrStateInvalid
				}
				seen[pid] = true
			}
			if a.PrimaryProxyID != "" && !seen[a.PrimaryProxyID] {
				return ErrStateInvalid
			}
			for _, p := range s.Proxies {
				if p.AccountID == id && !seen[p.ID] {
					return ErrStateInvalid
				}
			}
		}
		if a.Mode == "direct" {
			if a.PrimaryProxyID != "" {
				return ErrStateInvalid
			}
			for _, p := range s.Proxies {
				if p.AccountID == id {
					return ErrStateInvalid
				}
			}
		}
		if a.PrimaryProxyID != "" {
			p, ok := byID[a.PrimaryProxyID]
			if !ok || (p.AccountID != "" && p.AccountID != id) {
				return ErrStateInvalid
			}
		}
	}
	return nil
}
func (s *ProxyState) Sort() {
	sort.SliceStable(s.Proxies, func(i, j int) bool { return s.Proxies[i].Order < s.Proxies[j].Order })
}

type ProxyStateStore struct{ Dir string }

func (s ProxyStateStore) Load(initial ProxyState) (ProxyState, bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, StateFileName))
	if errors.Is(err, os.ErrNotExist) {
		if err = initial.Validate(); err != nil {
			return ProxyState{}, false, err
		}
		return initial, false, nil
	}
	if err != nil || !ojson.ValidBytes(raw) {
		return ProxyState{}, false, ErrStateStorage
	}
	v := ojson.ParseBytes(raw)
	revision, err := strconv.ParseUint(v.Get("revision").Raw, 10, 64)
	limit, errLimit := strconv.Atoi(v.Get("proxy_limit").Raw)
	if err != nil || revision == 0 || errLimit != nil || !v.Get("proxies").IsArray() || !v.Get("accounts").IsObject() {
		return ProxyState{}, false, ErrStateStorage
	}
	state := ProxyState{Revision: revision, Limit: limit, Accounts: map[string]AccountEgress{}}
	valid := true
	decodeProxy := func(item ojson.Result) SavedProxy {
		order, e := strconv.Atoi(item.Get("order").Raw)
		first, ef := strconv.ParseUint(item.Get("first_used").Raw, 10, 64)
		if e != nil || ef != nil || !item.Get("enabled").IsBool() {
			valid = false
		}
		for _, k := range []string{"id", "url", "name", "account_id"} {
			if item.Get(k).Type != ojson.String {
				valid = false
			}
		}
		p := SavedProxy{ID: item.Get("id").String(), URL: item.Get("url").String(), Name: item.Get("name").String(), AccountID: item.Get("account_id").String(), Order: order, Disabled: !item.Get("enabled").Bool(), FirstUsed: first, FixedIP: item.Get("fixed_exit_ip").String()}
		for _, field := range []string{"admission_status", "admission_reason", "admitted_ip"} {
			if x := item.Get(field); x.Exists() && x.Type != ojson.String {
				valid = false
			}
		}
		p.AdmissionStatus, p.AdmissionReason, p.AdmittedIP = item.Get("admission_status").String(), item.Get("admission_reason").String(), item.Get("admitted_ip").String()
		if x := item.Get("admitted_at"); x.Exists() && x.Type != ojson.Null {
			at, e := time.Parse(time.RFC3339Nano, x.String())
			if e != nil {
				valid = false
			}
			p.AdmittedAt = at
		}
		return p
	}
	for _, item := range v.Get("proxies").Array() {
		state.Proxies = append(state.Proxies, decodeProxy(item))
	}
	if recycled := v.Get("recycle"); recycled.Exists() {
		if !recycled.IsArray() {
			valid = false
		}
		for _, item := range recycled.Array() {
			archived, err := time.Parse(time.RFC3339Nano, item.Get("archived_at").String())
			if err != nil {
				valid = false
			}
			state.Recycle = append(state.Recycle, ArchivedProxy{Proxy: decodeProxy(item), ArchivedAt: archived})
		}
	}

	v.Get("accounts").ForEach(func(id, a ojson.Result) bool {
		if a.Get("mode").Type != ojson.String || a.Get("primary_proxy_id").Type != ojson.String {
			valid = false
		}
		candidates, err := DecodeCandidates(a.Get("candidate_proxy_ids"))
		if err != nil {
			valid = false
		}
		capacity, capacityErr := DecodeCapacityPolicy(a)
		if capacityErr != nil {
			valid = false
		}
		profiles := map[string]string{}
		if v := a.Get("device_profiles"); v.Exists() && v.Type != ojson.Null {
			if !v.IsObject() {
				valid = false
			}
			v.ForEach(func(k, p ojson.Result) bool {
				if k.String() == "" || p.Type != ojson.String || p.String() == "" {
					valid = false
				}
				profiles[k.String()] = p.String()
				return true
			})
		}
		state.Accounts[id.String()] = AccountEgress{DeviceProfiles: profiles, CapacityPolicy: capacity, Mode: a.Get("mode").String(), PrimaryProxyID: a.Get("primary_proxy_id").String(), CandidateProxyIDs: candidates}
		return true
	})
	if state.decodeAdmission(v) != nil {
		return ProxyState{}, false, ErrStateStorage
	}
	if state.decodeAuto(v) != nil {
		return ProxyState{}, false, ErrStateStorage
	}
	if !valid || state.Validate() != nil {
		return ProxyState{}, false, ErrStateStorage
	}

	if pending := v.Get("pending_device_saves"); pending.Exists() {
		if !pending.IsObject() {
			return ProxyState{}, false, ErrStateStorage
		}
		state.PendingDevices = map[string]PendingDeviceSave{}
		pending.ForEach(func(id, x ojson.Result) bool {
			d, err := DecodeInferenceEgress(x.Get("draft"))
			if err != nil || !x.Get("previous_primary").IsObject() {
				valid = false
				return false
			}
			p := PendingDeviceSave{Draft: d, PreviousPrimary: map[string]string{}}
			x.Get("previous_primary").ForEach(func(k, value ojson.Result) bool {
				if value.Type != ojson.String {
					valid = false
				}
				p.PreviousPrimary[k.String()] = value.String()
				return true
			})
			if _, ok := state.Accounts[id.String()]; !ok {
				valid = false
			}
			state.PendingDevices[id.String()] = p
			return true
		})
	}
	if !valid {
		return ProxyState{}, false, ErrStateStorage
	}
	state.Sort()
	return state, true, nil
}
func (s ProxyStateStore) Save(state ProxyState) error {
	if state.Validate() != nil {
		return ErrStateInvalid
	}
	encodeProxy := func(p SavedProxy) map[string]any {
		return map[string]any{"id": p.ID, "url": p.URL, "name": p.Name, "account_id": p.AccountID, "order": p.Order, "enabled": !p.Disabled, "first_used": p.FirstUsed, "fixed_exit_ip": p.FixedIP, "admission_status": p.AdmissionStatus, "admission_reason": p.AdmissionReason, "admitted_ip": p.AdmittedIP, "admitted_at": p.AdmissionView().AdmittedAt}
	}
	proxies := make([]any, 0, len(state.Proxies))
	for _, p := range state.Proxies {
		proxies = append(proxies, encodeProxy(p))
	}
	recycle := make([]any, 0, len(state.Recycle))
	for _, entry := range state.Recycle {
		value := encodeProxy(entry.Proxy)
		value["archived_at"] = entry.ArchivedAt.UTC().Format(time.RFC3339Nano)
		recycle = append(recycle, value)
	}

	accounts := map[string]any{}
	for id, a := range state.Accounts {
		accounts[id] = map[string]any{"device_profiles": a.DeviceProfiles, "mode": a.Mode, "primary_proxy_id": a.PrimaryProxyID, "candidate_proxy_ids": a.CandidateProxyIDs, "capacity_mode": a.Normalized().CapacityMode, "allocation_mode": a.Normalized().AllocationMode, "max_ips": a.Normalized().MaxIPs, "specified_ips": a.Normalized().SpecifiedIPs}
	}
	pending := map[string]any{}
	for id, p := range state.PendingDevices {
		draft := p.Draft
		draft.CapacityPolicy = draft.CapacityPolicy.Normalized()
		pending[id] = map[string]any{"draft": draft, "previous_primary": p.PreviousPrimary}
	}
	doc := ojson.NewDocString("{}")
	if doc.Set("ip_admission_history", state.IPHistory) != nil {
		return ErrStateStorage
	}
	if state.encodeAuto(doc) != nil {
		return ErrStateStorage
	}
	if doc.Set("pending_device_saves", pending) != nil {
		return ErrStateStorage
	}
	if doc.Set("revision", state.Revision) != nil || doc.Set("proxy_limit", state.Limit) != nil || doc.Set("proxies", proxies) != nil || doc.Set("accounts", accounts) != nil || doc.Set("recycle", recycle) != nil {
		return ErrStateStorage
	}
	if os.MkdirAll(s.Dir, 0700) != nil {
		return ErrStateStorage
	}
	f, err := os.CreateTemp(s.Dir, ".kilo-pool-")
	if err != nil {
		return ErrStateStorage
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	err = f.Chmod(0600)
	if err == nil {
		_, err = f.Write(doc.Bytes())
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = os.Rename(tmp, filepath.Join(s.Dir, StateFileName))
	}
	if err != nil {
		return ErrStateStorage
	}
	return nil
}

// ProxyChanges is the write-only management contract, never a response DTO.
type ProxyChanges struct {
	CapacityPolicy
	IPConcurrencyLimit    *int
	IPEntryLimit          *int
	SetIPEntryLimit       bool
	AutoAssignmentEnabled *bool
	MaxUniqueAccounts     *int
	FixedIP               *string
	ExpectedRevision      uint64
	ID                    string
	Create                bool
	Archive               bool
	ClearRecycle          bool
	URL, Name, AccountID  *string
	Order, Limit          *int
	Enabled               *bool
	EgressAccount         string
	Mode                  string
	ExclusiveProxyIDs     []string
	CandidateProxyIDs     []string
	PrimaryProxyID        string
}
type EgressAccountStatus struct {
	ManualLimit          *int
	CanAllocate          bool
	ReturnRequested      bool
	RestrictionPersisted bool
	CreatedAt            time.Time
	Provider             string
	Active               int
}
type ProxyView struct {
	ProxyAdmissionView
	InferenceActive   int        `json:"inference_active"`
	ManagementActive  int        `json:"management_active"`
	EffectiveLimit    int        `json:"effective_limit"`
	FixedIP           string     `json:"fixed_exit_ip"`
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Order             int        `json:"order"`
	Enabled           bool       `json:"enabled"`
	AccountID         string     `json:"account_id"`
	Scheme            string     `json:"scheme"`
	Host              string     `json:"host"`
	Port              string     `json:"port"`
	HasAuth           bool       `json:"has_auth"`
	Healthy           bool       `json:"healthy"`
	Active            int        `json:"active"`
	Owner             string     `json:"owner"`
	OwnerKind         string     `json:"owner_kind"`
	ObservedExitIP    *string    `json:"observed_exit_ip"`
	ObservedAt        *time.Time `json:"observed_at"`
	ObservationStatus string     `json:"observation_status"`
}
type AccountEgressView struct {
	CapacityPolicy
	AssignedIPs        []string `json:"assigned_ips"`
	ConfiguredCapacity int      `json:"configured_capacity"`
	CurrentCapacity    int      `json:"current_capacity"`
	InferenceActive    int      `json:"inference_active"`
	ManagementActive   int      `json:"management_active"`
	ManualLimit        *int     `json:"manual_limit"`
	EffectiveLimit     int      `json:"effective_limit"`
	EntryLimit         *int     `json:"entry_limit"`
	EgressReady        bool     `json:"egress_ready"`
	AssignedIP         string   `json:"assigned_ip"`
	AllocationState    string   `json:"allocation_state"`
	AllocationReason   string   `json:"allocation_reason"`
	AccountID          string   `json:"account_id"`
	Mode               string   `json:"mode"`
	PrimaryProxyID     string   `json:"primary_proxy_id"`
	CandidateProxyIDs  []string `json:"candidate_proxy_ids"`
}
type ProxyStateView struct {
	IPConcurrencyLimit    int                 `json:"ip_concurrency_limit"`
	IPEntryLimit          *int                `json:"ip_entry_limit"`
	AutoAssignmentEnabled bool                `json:"auto_assignment_enabled"`
	MaxUniqueAccounts     int                 `json:"max_unique_accounts"`
	IPGroups              []IPGroupView       `json:"ip_groups"`
	AutoError             *string             `json:"auto_error"`
	Revision              uint64              `json:"revision"`
	RuntimeRevision       uint64              `json:"runtime_revision"`
	Managed               bool                `json:"managed"`
	ProxyLimit            int                 `json:"proxy_limit"`
	Proxies               []ProxyView         `json:"proxies"`
	Accounts              []AccountEgressView `json:"accounts"`
	Recycle               []ArchivedProxyView `json:"recycle"`
}

// ArchivedProxyView omits the stored URL and does not imply current health.
type ArchivedProxyView struct {
	ProxyAdmissionView
	FixedIP           string `json:"fixed_exit_ip"`
	RetirementReason  string `json:"retirement_reason"`
	ID                string `json:"id"`
	Name              string `json:"name"`
	Order             int    `json:"order"`
	Enabled           bool   `json:"enabled"`
	OriginalAccountID string `json:"original_account_id"`
	Scheme            string `json:"scheme"`
	Host              string `json:"host"`
	Port              string `json:"port"`
	HasAuth           bool   `json:"has_auth"`
	ArchivedAt        string `json:"archived_at"`
}

// DecodeCandidates distinguishes the unrestricted default from an empty explicit set.
func DecodeCandidates(v ojson.Result) ([]string, error) {
	if !v.Exists() || v.Type == ojson.Null {
		return nil, nil
	}
	if !v.IsArray() || len(v.Array()) == 0 {
		return nil, ErrStateInvalid
	}
	out := []string{}
	seen := map[string]bool{}
	for _, x := range v.Array() {
		if x.Type != ojson.String || x.String() == "" || seen[x.String()] {
			return nil, ErrStateInvalid
		}
		seen[x.String()] = true
		out = append(out, x.String())
	}
	return out, nil
}

func (s ProxyState) validateAdmission() error {
	for ip, entry := range s.IPHistory {
		canonical, ok := FixedPublicIP(ip)
		if !ok || canonical != ip || entry.IP != ip || (entry.Source != "admission" && entry.Source != "legacy_confirmed") {
			return ErrStateInvalid
		}
		if entry.Source == "admission" && (len(entry.ProxyIDs) == 0 || entry.FirstAdmittedAt == nil) {
			return ErrStateInvalid
		}
		seen := map[string]bool{}
		for _, id := range entry.ProxyIDs {
			if id == "" || seen[id] {
				return ErrStateInvalid
			}
			seen[id] = true
		}
		if entry.FirstAdmittedAt != nil && entry.FirstAdmittedAt.IsZero() || entry.LastObservedAt != nil && entry.LastObservedAt.IsZero() {
			return ErrStateInvalid
		}
	}
	for _, p := range append(slices.Clone(s.Proxies), archivedProxies(s.Recycle)...) {
		if !slices.Contains([]string{"legacy", "pending", "admitted"}, p.AdmissionState()) {
			return ErrStateInvalid
		}
		if !slices.Contains([]string{"", "unverified", "probe_failed", "ip_unknown", "ip_changed", "not_public_ip", "duplicate_ip"}, p.AdmissionReason) {
			return ErrStateInvalid
		}
		if p.AdmissionState() != "pending" && p.AdmissionReason != "" {
			return ErrStateInvalid
		}
		if p.AdmissionState() == "legacy" && (p.AdmittedIP != "" || !p.AdmittedAt.IsZero()) {
			return ErrStateInvalid
		}
		if p.AdmittedIP != "" {
			ip, ok := FixedPublicIP(p.AdmittedIP)
			if !ok || ip != p.AdmittedIP || !slices.Contains(s.IPHistory[ip].ProxyIDs, p.ID) {
				return ErrStateInvalid
			}
		} else if !p.AdmittedAt.IsZero() {
			return ErrStateInvalid
		}
		if p.AdmissionState() == "admitted" && (p.AdmittedIP == "" || p.AdmittedAt.IsZero()) {
			return ErrStateInvalid
		}
	}
	return nil
}

func (s *ProxyState) decodeAdmission(v ojson.Result) error {
	s.IPHistory = map[string]ProxyIPRegistration{}
	history := v.Get("ip_admission_history")
	if !history.Exists() || history.Type == ojson.Null {
		return nil
	}
	if !history.IsObject() {
		return ErrStateStorage
	}
	valid := true
	history.ForEach(func(key, item ojson.Result) bool {
		entry := ProxyIPRegistration{IP: item.Get("ip").String(), Source: item.Get("source").String()}
		if !item.IsObject() || !item.Get("proxy_ids").IsArray() {
			valid = false
			return false
		}
		for _, id := range item.Get("proxy_ids").Array() {
			if id.Type != ojson.String {
				valid = false
			}
			entry.ProxyIDs = append(entry.ProxyIDs, id.String())
		}
		for _, field := range []string{"first_admitted_at", "last_observed_at"} {
			if x := item.Get(field); x.Exists() && x.Type != ojson.Null {
				at, err := time.Parse(time.RFC3339Nano, x.String())
				if err != nil {
					valid = false
				}
				if field == "first_admitted_at" {
					entry.FirstAdmittedAt = &at
				} else {
					entry.LastObservedAt = &at
				}
			}
		}
		s.IPHistory[key.String()] = entry
		return true
	})
	if !valid {
		return ErrStateStorage
	}
	return nil
}
