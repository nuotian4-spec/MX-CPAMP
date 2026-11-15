package model

type KiloTestModel struct {
	ID string `json:"id"`
}

type KiloTestModels struct {
	Models        []KiloTestModel `json:"models"`
	ConnectionRef string          `json:"connection_ref"`
}

type KiloManualTest struct {
	AccountID     string  `json:"account_id"`
	ConnectionRef string  `json:"connection_ref"`
	Model         string  `json:"model"`
	Status        string  `json:"status"`
	StartedAt     string  `json:"started_at"`
	CompletedAt   string  `json:"completed_at"`
	DurationMS    int64   `json:"duration_ms"`
	Text          string  `json:"text"`
	ErrorCode     *string `json:"error_code"`
}
