package model

type KiloAuthorizationEgress struct {
	Mode    string `json:"mode"`
	ProxyID string `json:"proxy_id,omitempty"`
}
type KiloInferenceEgress struct {
	CapacityMode   string   `json:"capacity_mode,omitempty"`
	AllocationMode string   `json:"allocation_mode,omitempty"`
	MaxIPs         *int     `json:"max_ips,omitempty"`
	SpecifiedIPs   []string `json:"specified_ips"`

	Mode              string   `json:"mode"`
	CandidateProxyIDs []string `json:"candidate_proxy_ids"`
	ExclusiveProxyIDs []string `json:"exclusive_proxy_ids"`
	PrimaryProxyID    string   `json:"primary_proxy_id"`
	ExpectedRevision  uint64   `json:"expected_revision"`
}

type KiloDeviceStarted struct {
	AuthorizationEgress *KiloAuthorizationEgress `json:"authorization_egress,omitempty"`
	InferenceEgress     *KiloInferenceEgress     `json:"inference_egress,omitempty"`
	ID                  string                   `json:"id"`
	UserCode            string                   `json:"user_code"`
	VerificationURL     string                   `json:"verification_url"`
	ExpiresIn           int                      `json:"expires_in"`
	Interval            int                      `json:"interval"`
	InferenceEgressMode string                   `json:"inference_egress_mode"`
	ConnectionRef       string                   `json:"connection_ref"`
}

type KiloDeviceState struct {
	AuthorizationEgress *KiloAuthorizationEgress `json:"authorization_egress,omitempty"`
	InferenceEgress     *KiloInferenceEgress     `json:"inference_egress,omitempty"`
	Status              string                   `json:"status"`
	Email               string                   `json:"email,omitempty"`
	SaveExpiresIn       *int                     `json:"save_expires_in,omitempty"`
	AccountID           string                   `json:"account_id,omitempty"`
	ConnectionRef       string                   `json:"connection_ref"`
}

type KiloDeviceSaved struct {
	CredentialSaved     *bool                    `json:"credential_saved,omitempty"`
	EgressReady         *bool                    `json:"egress_ready,omitempty"`
	EgressError         *string                  `json:"egress_error"`
	AuthorizationEgress *KiloAuthorizationEgress `json:"authorization_egress,omitempty"`
	InferenceEgress     *KiloInferenceEgress     `json:"inference_egress,omitempty"`
	KiloAccount
	EgressMode    string `json:"egress_mode"`
	EgressStatus  string `json:"egress_status"`
	EgressReason  string `json:"egress_reason"`
	ConnectionRef string `json:"connection_ref"`
}

// KiloDeviceResult仅用于内部传递，不直接序列化到客户端。
type KiloDeviceResult struct {
	Started *KiloDeviceStarted
	State   *KiloDeviceState
	Saved   *KiloDeviceSaved
}
