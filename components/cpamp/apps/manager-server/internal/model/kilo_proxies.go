package model

import (
	"encoding/json"
	"errors"
)

type KiloProxyAdmission struct {
	AdmissionStatus string  `json:"admission_status"`
	AdmissionReason string  `json:"admission_reason"`
	AdmittedIP      string  `json:"admitted_ip"`
	AdmittedAt      *string `json:"admitted_at"`
}

type KiloProxyPreview struct {
	Status            string  `json:"status"`
	Reason            string  `json:"reason"`
	ObservedExitIP    *string `json:"observed_exit_ip"`
	ObservedAt        *string `json:"observed_at"`
	RegisteredProxyID string  `json:"registered_proxy_id,omitempty"`
}

type KiloProxy struct {
	KiloProxyAdmission
	InferenceActive   *int    `json:"inference_active,omitempty"`
	ManagementActive  *int    `json:"management_active,omitempty"`
	EffectiveLimit    *int    `json:"effective_limit,omitempty"`
	FixedExitIP       string  `json:"fixed_exit_ip"`
	ObservedExitIP    *string `json:"observed_exit_ip"`
	ObservedAt        *string `json:"observed_at"`
	ObservationStatus string  `json:"observation_status"`
	OwnerKind         string  `json:"owner_kind"`
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Order             int     `json:"order"`
	Enabled           bool    `json:"enabled"`
	AccountID         string  `json:"account_id"`
	Scheme            string  `json:"scheme"`
	Host              string  `json:"host"`
	Port              string  `json:"port"`
	HasAuth           bool    `json:"has_auth"`
	Healthy           bool    `json:"healthy"`
	Active            int     `json:"active"`
	Owner             string  `json:"owner"`
}

type KiloRecycledProxy struct {
	KiloProxyAdmission
	FixedExitIP       string `json:"fixed_exit_ip"`
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

type KiloAccountEgress struct {
	CapacityMode       string   `json:"capacity_mode,omitempty"`
	AllocationMode     string   `json:"allocation_mode,omitempty"`
	MaxIPs             *int     `json:"max_ips,omitempty"`
	SpecifiedIPs       []string `json:"specified_ips"`
	AssignedIPs        []string `json:"assigned_ips"`
	ConfiguredCapacity *int     `json:"configured_capacity,omitempty"`
	CurrentCapacity    *int     `json:"current_capacity,omitempty"`
	InferenceActive    *int     `json:"inference_active,omitempty"`
	ManagementActive   *int     `json:"management_active,omitempty"`
	ManualLimit        *int     `json:"manual_limit"`
	EffectiveLimit     *int     `json:"effective_limit,omitempty"`
	EntryLimit         *int     `json:"entry_limit"`
	EgressReady        *bool    `json:"egress_ready,omitempty"`

	AssignedIP        string   `json:"assigned_ip"`
	AllocationState   string   `json:"allocation_state"`
	AllocationReason  string   `json:"allocation_reason"`
	CandidateProxyIDs []string `json:"candidate_proxy_ids"`
	AccountID         string   `json:"account_id"`
	Mode              string   `json:"mode"`
	PrimaryProxyID    string   `json:"primary_proxy_id"`
}

type KiloProxyState struct {
	IPConcurrencyLimit    *int                `json:"ip_concurrency_limit,omitempty"`
	IPEntryLimit          *int                `json:"ip_entry_limit"`
	AutoAssignmentEnabled *bool               `json:"auto_assignment_enabled,omitempty"`
	MaxUniqueAccounts     *int                `json:"max_unique_accounts,omitempty"`
	AutoError             *string             `json:"auto_error"`
	IPGroups              *[]KiloIPGroup      `json:"ip_groups,omitempty"`
	Revision              uint64              `json:"revision"`
	RuntimeRevision       uint64              `json:"runtime_revision"`
	Managed               bool                `json:"managed"`
	ProxyLimit            int                 `json:"proxy_limit"`
	Recycle               []KiloRecycledProxy `json:"recycle"`
	Proxies               []KiloProxy         `json:"proxies"`
	Accounts              []KiloAccountEgress `json:"accounts"`
	ConnectionRef         string              `json:"connection_ref,omitempty"`
}

type KiloIPGroup struct {
	Active            *int     `json:"active,omitempty"`
	InferenceActive   *int     `json:"inference_active,omitempty"`
	ManagementActive  *int     `json:"management_active,omitempty"`
	Capacity          *int     `json:"capacity,omitempty"`
	EntryLimit        *int     `json:"entry_limit"`
	IP                string   `json:"ip"`
	OwnerAccountID    string   `json:"owner_account_id"`
	AccountIDs        []string `json:"account_ids"`
	Draining          bool     `json:"draining"`
	Retired           bool     `json:"retired"`
	Reason            string   `json:"reason"`
	UniqueAccounts    int      `json:"unique_accounts"`
	MaxUniqueAccounts int      `json:"max_unique_accounts"`
	State             string   `json:"state"`
	ProxyIDs          []string `json:"proxy_ids"`
}

func (g *KiloIPGroup) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("IP归组回执不是对象")
	}
	for _, key := range []string{"ip", "owner_account_id", "account_ids", "draining", "retired", "reason", "unique_accounts", "max_unique_accounts", "state", "proxy_ids"} {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return errors.New("IP归组回执字段缺失")
		}
	}
	type plain KiloIPGroup
	return json.Unmarshal(raw, (*plain)(g))
}
