package kilo

import (
	"github.com/MoeForge/moego/ojson"
	"slices"
	"strconv"
)

const DefaultIPConcurrencyLimit = 20

// CapacityPolicy opts an account into capacity supplied by its exclusive IPs.
type CapacityPolicy struct {
	CapacityMode   string   `json:"capacity_mode"`
	AllocationMode string   `json:"allocation_mode"`
	MaxIPs         int      `json:"max_ips"`
	SpecifiedIPs   []string `json:"specified_ips"`
}

func (p CapacityPolicy) Normalized() CapacityPolicy {
	if p.CapacityMode == "" {
		p.CapacityMode = "fixed"
	}
	if p.AllocationMode == "" {
		p.AllocationMode = "auto"
	}
	if p.MaxIPs == 0 {
		p.MaxIPs = 1
	}
	p.SpecifiedIPs = append([]string{}, p.SpecifiedIPs...)
	return p
}
func (p CapacityPolicy) Validate(mode string) error {
	p = p.Normalized()
	if p.CapacityMode != "fixed" && p.CapacityMode != "ip" {
		return ErrStateInvalid
	}
	if p.AllocationMode != "auto" && p.AllocationMode != "specified" {
		return ErrStateInvalid
	}
	if p.MaxIPs < 1 || p.MaxIPs > 21474836 {
		return ErrStateInvalid
	}
	if p.CapacityMode == "ip" && mode != "auto" {
		return ErrStateInvalid
	}
	if p.CapacityMode == "fixed" && (p.AllocationMode != "auto" || p.MaxIPs != 1 || len(p.SpecifiedIPs) > 0) {
		return ErrStateInvalid
	}
	if p.AllocationMode == "specified" && (len(p.SpecifiedIPs) == 0 || len(p.SpecifiedIPs) > p.MaxIPs) {
		return ErrStateInvalid
	}
	if p.AllocationMode == "auto" && len(p.SpecifiedIPs) > 0 {
		return ErrStateInvalid
	}
	seen := map[string]bool{}
	for _, ip := range p.SpecifiedIPs {
		canonical, ok := FixedPublicIP(ip)
		if !ok || canonical != ip || seen[ip] {
			return ErrStateInvalid
		}
		seen[ip] = true
	}
	return nil
}
func DecodeCapacityPolicy(v ojson.Result) (CapacityPolicy, error) {
	p := CapacityPolicy{}
	for _, k := range []string{"capacity_mode", "allocation_mode"} {
		x := v.Get(k)
		if x.Exists() {
			if x.Type != ojson.String {
				return p, ErrStateInvalid
			}
			if k == "capacity_mode" {
				p.CapacityMode = x.String()
			} else {
				p.AllocationMode = x.String()
			}
		}
	}
	if x := v.Get("max_ips"); x.Exists() {
		n, e := strconv.Atoi(x.Raw)
		if e != nil || n < 1 {
			return p, ErrStateInvalid
		}
		p.MaxIPs = n
	}
	if x := v.Get("specified_ips"); x.Exists() {
		if !x.IsArray() {
			return p, ErrStateInvalid
		}
		for _, ip := range x.Array() {
			if ip.Type != ojson.String {
				return p, ErrStateInvalid
			}
			p.SpecifiedIPs = append(p.SpecifiedIPs, ip.String())
		}
	}
	return p, p.Validate(v.Get("mode").String())
}
func (s ProxyState) IPCapacity() int {
	if s.IPConcurrencyLimit > 0 {
		return s.IPConcurrencyLimit
	}
	return DefaultIPConcurrencyLimit
}
func (s ProxyState) OwnedIPs(id string) []string {
	ips := []string{}
	for ip, g := range s.IPGroups {
		if g.Owner == id {
			ips = append(ips, ip)
		}
	}
	slices.Sort(ips)
	return ips
}
