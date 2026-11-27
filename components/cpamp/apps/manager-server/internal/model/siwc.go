package model

type SIWCIdentity struct {
	ConnectionRef string `json:"connection_ref"`
	AuthID        string `json:"auth_id"`
	AuthIndex     string `json:"auth_index"`
}

type SIWCCredential struct {
	SIWCIdentity
	Provider          string `json:"provider"`
	SIWCBindSupported bool   `json:"siwc_bind_supported"`
	UseSIWC           bool   `json:"use_siwc"`
	SIWCStatus        string `json:"siwc_status"`
	SIWCBound         bool   `json:"siwc_bound"`
	SIWCExpiresAt     string `json:"siwc_expires_at,omitempty"`
	ReadOnlyReason    string `json:"read_only_reason,omitempty"`
}

type SIWCBindResult struct {
	SIWCIdentity
	Status    string `json:"status"`
	Flow      string `json:"flow"`
	URL       string `json:"url"`
	State     string `json:"state"`
	ExpiresIn int    `json:"expires_in"`
}

type SIWCSessionInput struct {
	SIWCIdentity
	State string `json:"state"`
}

type SIWCCallbackInput struct {
	SIWCSessionInput
	RedirectURL string `json:"redirect_url"`
}

type SIWCSessionResult struct {
	Status     string          `json:"status"`
	Credential *SIWCCredential `json:"credential,omitempty"`
	Code       string          `json:"code,omitempty"`
}

type SIWCCancelResult struct {
	SIWCSessionResult
	Cancelled bool `json:"cancelled"`
}
