package model

type KiloProbeState struct {
	Enabled         bool    `json:"enabled"`
	Model           string  `json:"model"`
	IntervalSeconds int     `json:"interval_seconds"`
	Status          string  `json:"status"`
	NextRunAt       *string `json:"next_run_at"`
	LastStartedAt   *string `json:"last_started_at"`
	LastCompletedAt *string `json:"last_completed_at"`
	ErrorCode       *string `json:"error_code"`
	ModelCalls      int64   `json:"model_calls"`
	SkippedCount    int64   `json:"skipped_count"`
}

type KiloProbes struct {
	AccountID        string         `json:"account_id"`
	Revision         int64          `json:"revision"`
	RuntimeStartedAt string         `json:"runtime_started_at"`
	ManualModelCalls int64          `json:"manual_model_calls"`
	Health           KiloProbeState `json:"health"`
	ModelProbe       KiloProbeState `json:"model_probe"`
	ConnectionRef    string         `json:"connection_ref"`
}
