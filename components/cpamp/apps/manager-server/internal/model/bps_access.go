package model

// BPSAccess is a read-only permission observation, not inference or quota evidence.
type BPSAccess struct {
	Provider            string   `json:"provider"`
	AuthIndex           string   `json:"auth_index"`
	AuthID              string   `json:"auth_id"`
	QueriedAt           string   `json:"queried_at"`
	Allowed             *bool    `json:"allowed"`
	Models              []string `json:"models"`
	UpstreamStatus      *int     `json:"upstream_status"`
	Error               *string  `json:"error"`
	Quota               *float64 `json:"quota"`
	InferenceVerified   bool     `json:"inference_verified"`
	AutoRecoverEligible bool     `json:"auto_recover_eligible"`
}
