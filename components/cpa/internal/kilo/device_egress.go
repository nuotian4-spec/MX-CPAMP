package kilo

import (
	"github.com/MoeForge/moego/ojson"
	"slices"
	"strconv"
)

// AuthorizationEgress identifies the fixed proxy used by one device flow.
type AuthorizationEgress struct {
	Mode             string  `json:"mode"`
	ProxyID          string  `json:"proxy_id,omitempty"`
	ExpectedRevision *uint64 `json:"expected_revision,omitempty"`
}

// InferenceEgress is the public device-session draft, containing no credentials.
type InferenceEgress struct {
	CapacityPolicy
	Default           bool     `json:"-"`
	Mode              string   `json:"mode"`
	CandidateProxyIDs []string `json:"candidate_proxy_ids"`
	ExclusiveProxyIDs []string `json:"exclusive_proxy_ids"`
	PrimaryProxyID    string   `json:"primary_proxy_id"`
	ExpectedRevision  *uint64  `json:"expected_revision,omitempty"`
}

// PendingDeviceSave records only the local policy changes preceding credential publication.
type PendingDeviceSave struct {
	Draft           InferenceEgress
	PreviousPrimary map[string]string
}

func (p PendingDeviceSave) Clone() PendingDeviceSave {
	p.Draft.SpecifiedIPs = slices.Clone(p.Draft.SpecifiedIPs)
	p.Draft.CandidateProxyIDs = slices.Clone(p.Draft.CandidateProxyIDs)
	p.Draft.ExclusiveProxyIDs = slices.Clone(p.Draft.ExclusiveProxyIDs)
	old := p.PreviousPrimary
	p.PreviousPrimary = map[string]string{}
	for id, pid := range old {
		p.PreviousPrimary[id] = pid
	}
	return p
}
func DecodeInferenceEgress(v ojson.Result) (InferenceEgress, error) {
	d := InferenceEgress{Mode: "pool", ExclusiveProxyIDs: []string{}}
	if !v.Exists() {
		d.Default = true
		return d, nil
	}
	if !v.IsObject() {
		return d, ErrStateInvalid
	}
	valid := true
	v.ForEach(func(k, x ojson.Result) bool {
		switch k.String() {
		case "capacity_mode", "allocation_mode", "max_ips", "specified_ips":
		case "mode":
			if x.Type != ojson.String {
				valid = false
			}
			d.Mode = x.String()
		case "primary_proxy_id":
			if x.Type != ojson.String {
				valid = false
			}
			d.PrimaryProxyID = x.String()
		case "candidate_proxy_ids":
			var err error
			d.CandidateProxyIDs, err = DecodeCandidates(x)
			valid = valid && err == nil
		case "exclusive_proxy_ids":
			if !x.IsArray() {
				valid = false
			}
			seen := map[string]bool{}
			for _, id := range x.Array() {
				if id.Type != ojson.String || id.String() == "" || seen[id.String()] {
					valid = false
				}
				seen[id.String()] = true
				d.ExclusiveProxyIDs = append(d.ExclusiveProxyIDs, id.String())
			}
		case "expected_revision":
			n, err := strconv.ParseUint(x.Raw, 10, 64)
			if err != nil {
				valid = false
			}
			d.ExpectedRevision = &n
		default:
			valid = false
		}
		return true
	})
	capacity, err := DecodeCapacityPolicy(v)
	if err != nil {
		valid = false
	}
	d.CapacityPolicy = capacity
	if d.Mode != "pool" && d.Mode != "direct" && d.Mode != "auto" {
		valid = false
	}
	if (d.Mode == "direct" || d.Mode == "auto") && (d.CandidateProxyIDs != nil || len(d.ExclusiveProxyIDs) > 0 || d.PrimaryProxyID != "") {
		valid = false
	}
	if !valid {
		return d, ErrStateInvalid
	}
	return d, nil
}
func DecodeAuthorizationEgress(v ojson.Result) (AuthorizationEgress, error) {
	a := AuthorizationEgress{}
	valid := v.IsObject()
	v.ForEach(func(k, x ojson.Result) bool {
		switch k.String() {
		case "mode":
			if x.Type != ojson.String {
				valid = false
			}
			a.Mode = x.String()
		case "proxy_id":
			if x.Type != ojson.String {
				valid = false
			}
			a.ProxyID = x.String()
		case "expected_revision":
			n, err := strconv.ParseUint(x.Raw, 10, 64)
			if err != nil {
				valid = false
			}
			a.ExpectedRevision = &n
		default:
			valid = false
		}
		return true
	})
	if a.Mode != "pool" && a.Mode != "direct" {
		valid = false
	}
	if a.Mode == "direct" && a.ProxyID != "" {
		valid = false
	}
	if a.ProxyID != "" && a.ExpectedRevision == nil {
		valid = false
	}
	if !valid {
		return a, ErrStateInvalid
	}
	return a, nil
}
