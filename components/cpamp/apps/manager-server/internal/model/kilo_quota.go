package model

type KiloQuotaSourceOption struct {
	CredentialID     string  `json:"credential_id"`
	Label            string  `json:"label"`
	ChatGPTAccountID *string `json:"chatgpt_account_id"`
	Status           string  `json:"status"`
	HasQueryMaterial bool    `json:"has_query_material"`
}
type KiloQuotaSources struct {
	Sources       []KiloQuotaSourceOption `json:"sources"`
	ConnectionRef string                  `json:"connection_ref"`
}
type KiloQuotaSource struct {
	Kind         string `json:"kind"`
	CredentialID string `json:"credential_id,omitempty"`
	Status       string `json:"status"`
}
type KiloQuotaQuery struct {
	Status      string  `json:"status"`
	AttemptedAt *string `json:"attempted_at"`
	ObservedAt  *string `json:"observed_at"`
	ErrorCode   *string `json:"error_code"`
}
type KiloQuotaWindow struct {
	Group            string   `json:"group"`
	Slot             string   `json:"slot"`
	DurationSeconds  *float64 `json:"duration_seconds"`
	RemainingPercent *float64 `json:"remaining_percent"`
	UsedPercent      *float64 `json:"used_percent"`
	ResetAt          *string  `json:"reset_at"`
	Source           string   `json:"source"`
}
type KiloQuotaCredits struct {
	Remaining   *float64 `json:"remaining"`
	SourceField *string  `json:"source_field"`
	Unit        *string  `json:"unit"`
}
type KiloQuotaView struct {
	AccountID       string            `json:"account_id"`
	BindingRevision uint64            `json:"binding_revision"`
	Source          KiloQuotaSource   `json:"source"`
	Query           KiloQuotaQuery    `json:"query"`
	Plan            *string           `json:"plan"`
	Windows         []KiloQuotaWindow `json:"windows"`
	Credits         KiloQuotaCredits  `json:"credits"`
	Stale           bool              `json:"stale"`
	ConnectionRef   string            `json:"connection_ref"`
}
type KiloQuotaResult struct {
	Status  int
	Policy  *KiloSourcePolicy
	Sources *KiloQuotaSources
	View    *KiloQuotaView
}
