package model

import (
	"encoding/json"
	"errors"
)

type KiloClientIdentity struct {
	State    string                      `json:"state"`
	Profiles []KiloClientIdentityProfile `json:"profiles"`
}

type KiloClientIdentityProfile struct {
	Ref   string                    `json:"ref"`
	Exits *[]KiloClientIdentityExit `json:"exits,omitempty"`
}

type KiloClientIdentityExit struct {
	Kind string  `json:"kind"`
	IP   *string `json:"ip,omitempty"`
}

func (profile *KiloClientIdentityProfile) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Ref   string          `json:"ref"`
		Exits json.RawMessage `json:"exits"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	profile.Ref, profile.Exits = wire.Ref, nil
	if len(wire.Exits) > 0 {
		var exits []KiloClientIdentityExit
		if err := json.Unmarshal(wire.Exits, &exits); err != nil {
			return err
		}
		if exits == nil {
			return errors.New("身份出口关联应为数组")
		}
		profile.Exits = &exits
	}
	return nil
}

func (exit *KiloClientIdentityExit) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Kind string          `json:"kind"`
		IP   json.RawMessage `json:"ip"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	exit.Kind, exit.IP = wire.Kind, nil
	if len(wire.IP) > 0 {
		if err := json.Unmarshal(wire.IP, &exit.IP); err != nil {
			return err
		}
		if wire.Kind != "ip" || exit.IP == nil {
			return errors.New("身份出口 IP 结构无效")
		}
	}
	return nil
}

type KiloQuotaSourceSummary struct {
	Kind            string `json:"kind"`
	CredentialID    string `json:"credential_id"`
	DisplayLabel    string `json:"display_label"`
	Status          string `json:"status"`
	BindingRevision uint64 `json:"binding_revision"`
}

type KiloAccount struct {
	ClientIdentity           *KiloClientIdentity     `json:"client_identity,omitempty"`
	SourcePolicy             *KiloSourcePolicy       `json:"source_policy,omitempty"`
	QuotaSourceSummary       *KiloQuotaSourceSummary `json:"quota_source_summary,omitempty"`
	QuotaSnapshot            *KiloQuotaView          `json:"quota_snapshot,omitempty"`
	AutoRestriction          *KiloAutoRestriction    `json:"auto_restriction,omitempty"`
	ID                       string                  `json:"id"`
	Name                     string                  `json:"name"`
	Enabled                  bool                    `json:"enabled"`
	Priority                 int                     `json:"priority"`
	OrganizationID           *string                 `json:"organization_id,omitempty"`
	AllowDirectWhenProxyFull bool                    `json:"allow_direct_when_proxy_full"`
	ConcurrencyLimit         *int                    `json:"concurrency_limit"`
	EffectiveLimit           int                     `json:"effective_limit"`
	Active                   int                     `json:"active"`
	Waiting                  int                     `json:"waiting"`
	QuotaSource              string                  `json:"quota_source"`
	CodexCredentialID        string                  `json:"codex_credential_id"`
}

type KiloAccounts struct {
	ConnectionRef string        `json:"connection_ref,omitempty"`
	Accounts      []KiloAccount `json:"accounts"`
}

type KiloAccountSaved struct {
	KiloAccount
	ConnectionRef string `json:"connection_ref"`
}

type KiloStatus struct {
	ConnectionRef           string `json:"connection_ref,omitempty"`
	Configured              bool   `json:"configured"`
	AccountCount            int    `json:"account_count"`
	HealthyProxyCount       int    `json:"healthy_proxy_count"`
	BusyProxyCount          int    `json:"busy_proxy_count"`
	AutoAccountProbeEnabled bool   `json:"auto_account_probe_enabled"`
	AutoModelProbeEnabled   bool   `json:"auto_model_probe_enabled"`
}

type KiloCapabilities struct {
	ConnectionRef           string   `json:"connection_ref,omitempty"`
	Channel                 string   `json:"channel"`
	PublicProtocols         []string `json:"public_protocols"`
	ToolsSemantics          string   `json:"tools_semantics"`
	ImagesSemantics         string   `json:"images_semantics"`
	AutoAccountProbeDefault bool     `json:"auto_account_probe_default"`
	AutoModelProbeDefault   bool     `json:"auto_model_probe_default"`
	UpstreamBillingVerified bool     `json:"upstream_billing_verified"`
}
