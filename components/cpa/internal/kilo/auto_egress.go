package kilo

import (
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/MoeForge/moego/ojson"
)

// IPGroup retains formal assignment history even after every entry is archived.
type IPGroup struct {
	IP         string   `json:"ip"`
	Owner      string   `json:"owner_account_id"`
	AccountIDs []string `json:"account_ids"`
	Draining   bool     `json:"draining"`
	Retired    bool     `json:"retired"`
	Reason     string   `json:"reason"`
}
type IPGroupView struct {
	InferenceActive  int  `json:"inference_active"`
	ManagementActive int  `json:"management_active"`
	Active           int  `json:"active"`
	Capacity         int  `json:"capacity"`
	EntryLimit       *int `json:"entry_limit"`
	IPGroup
	UniqueAccounts    int      `json:"unique_accounts"`
	MaxUniqueAccounts int      `json:"max_unique_accounts"`
	State             string   `json:"state"`
	ProxyIDs          []string `json:"proxy_ids"`
}

func FixedPublicIP(raw string) (string, bool) {
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return "", false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", false
	}
	return ip.String(), true
}
func (s ProxyState) validateAuto() error {
	if s.MaxUniqueAccounts < 0 || (s.AutoAssignmentEnabled && s.MaxUniqueAccounts < 1) {
		return ErrStateInvalid
	}
	for ip, g := range s.IPGroups {
		canonical, ok := FixedPublicIP(ip)
		if !ok || canonical != ip || g.IP != ip || (g.Retired && g.Owner != "") || (g.Draining && g.Owner == "") {
			return ErrStateInvalid
		}
		seen := map[string]bool{}
		for _, id := range g.AccountIDs {
			if id == "" || seen[id] {
				return ErrStateInvalid
			}
			seen[id] = true
		}
		if g.Owner != "" {
			if !seen[g.Owner] {
				return ErrStateInvalid
			}
		}
	}
	for _, p := range append(slices.Clone(s.Proxies), archivedProxies(s.Recycle)...) {
		if p.FixedIP != "" {
			if p.AccountID != "" {
				return ErrStateInvalid
			}
			if _, ok := s.IPGroups[p.FixedIP]; !ok {
				return ErrStateInvalid
			}
		}
	}
	return nil
}
func archivedProxies(entries []ArchivedProxy) []SavedProxy {
	out := []SavedProxy{}
	for _, e := range entries {
		out = append(out, e.Proxy)
	}
	return out
}
func (s *ProxyState) decodeAuto(v ojson.Result) error {
	s.IPGroups = map[string]IPGroup{}
	if x := v.Get("ip_concurrency_limit"); x.Exists() {
		n, e := strconv.Atoi(x.Raw)
		if e != nil || validateLimit(n) != nil {
			return ErrStateStorage
		}
		s.IPConcurrencyLimit = n
	}
	if x := v.Get("ip_entry_limit"); x.Exists() && x.Type != ojson.Null {
		n, e := strconv.Atoi(x.Raw)
		if e != nil || validateLimit(n) != nil {
			return ErrStateStorage
		}
		s.IPEntryLimit = &n
	}
	if x := v.Get("auto_assignment_enabled"); x.Exists() {
		if !x.IsBool() {
			return ErrStateStorage
		}
		s.AutoAssignmentEnabled = x.Bool()
	}
	if x := v.Get("max_unique_accounts"); x.Exists() {
		n, e := strconv.Atoi(x.Raw)
		if e != nil || n < 0 {
			return ErrStateStorage
		}
		s.MaxUniqueAccounts = n
	}
	if x := v.Get("auto_enabled_at").String(); x != "" {
		at, e := time.Parse(time.RFC3339Nano, x)
		if e != nil {
			return ErrStateStorage
		}
		s.AutoEnabledAt = at
	}
	groups := v.Get("ip_groups")
	if groups.Exists() && !groups.IsObject() {
		return ErrStateStorage
	}
	valid := true
	groups.ForEach(func(k, x ojson.Result) bool {
		g := IPGroup{IP: k.String(), Owner: x.Get("owner_account_id").String(), Draining: x.Get("draining").Bool(), Retired: x.Get("retired").Bool(), Reason: x.Get("reason").String(), AccountIDs: []string{}}
		if !x.IsObject() || !x.Get("account_ids").IsArray() || !x.Get("draining").IsBool() || !x.Get("retired").IsBool() {
			valid = false
			return false
		}
		for _, a := range x.Get("account_ids").Array() {
			if a.Type != ojson.String {
				valid = false
			}
			g.AccountIDs = append(g.AccountIDs, a.String())
		}
		s.IPGroups[k.String()] = g
		return true
	})
	if !valid {
		return ErrStateStorage
	}
	return s.validateAuto()
}
func (s ProxyState) encodeAuto(doc *ojson.Doc) error {
	if doc.Set("ip_concurrency_limit", s.IPCapacity()) != nil || doc.Set("ip_entry_limit", s.IPEntryLimit) != nil {
		return ErrStateStorage
	}
	groups := map[string]IPGroup{}
	for ip, group := range s.IPGroups {
		group.AccountIDs = append([]string{}, group.AccountIDs...)
		groups[ip] = group
	}
	at := ""
	if !s.AutoEnabledAt.IsZero() {
		at = s.AutoEnabledAt.UTC().Format(time.RFC3339Nano)
	}
	if doc.Set("auto_assignment_enabled", s.AutoAssignmentEnabled) != nil || doc.Set("max_unique_accounts", s.MaxUniqueAccounts) != nil || doc.Set("auto_enabled_at", at) != nil || doc.Set("ip_groups", groups) != nil {
		return ErrStateStorage
	}
	return nil
}
