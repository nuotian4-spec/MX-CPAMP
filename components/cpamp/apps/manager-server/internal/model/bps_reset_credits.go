package model

type BPSResetCredits struct {
	Provider  string   `json:"provider"`
	AuthID    string   `json:"auth_id"`
	AuthIndex string   `json:"auth_index"`
	Source    string   `json:"source"`
	QueryMode string   `json:"query_mode"`
	QueriedAt string   `json:"queried_at"`
	Count     *float64 `json:"count"`
	Status    string   `json:"status"`
	Error     *string  `json:"error"`
}
