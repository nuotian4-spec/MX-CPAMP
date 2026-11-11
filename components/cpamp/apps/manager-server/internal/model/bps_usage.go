package model

import "time"

// BPSUsage 表示官方账号用量，不推断 BPS 模型的额度归属。
type BPSUsage struct {
	Provider       string             `json:"provider"`
	AuthIndex      string             `json:"auth_index"`
	AuthID         string             `json:"auth_id"`
	Source         string             `json:"source"`
	QueriedAt      string             `json:"queried_at"`
	UpstreamStatus *int               `json:"upstream_status"`
	Error          *string            `json:"error"`
	PlanType       *string            `json:"plan_type"`
	RateLimit      *BPSUsageRateLimit `json:"rate_limit"`
}

type BPSUsageRateLimit struct {
	Allowed         *bool           `json:"allowed"`
	LimitReached    *bool           `json:"limit_reached"`
	PrimaryWindow   *BPSUsageWindow `json:"primary_window"`
	SecondaryWindow *BPSUsageWindow `json:"secondary_window"`
}

type BPSUsageWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *float64 `json:"limit_window_seconds"`
	ResetAfterSeconds  *float64 `json:"reset_after_seconds"`
	ResetAt            *float64 `json:"reset_at"`
}

func (u BPSUsage) QuotaWindows() []CodexInspectionQuotaWindow {
	windows := []CodexInspectionQuotaWindow{}
	if u.Error != nil || u.RateLimit == nil {
		return windows
	}
	observed, _ := time.Parse(time.RFC3339, u.QueriedAt)
	for i, w := range []*BPSUsageWindow{u.RateLimit.PrimaryWindow, u.RateLimit.SecondaryWindow} {
		if w == nil {
			continue
		}
		reset := int64(0)
		accuracy := "unknown"
		if w.ResetAt != nil && *w.ResetAt > 0 {
			reset = int64(*w.ResetAt * 1000)
			accuracy = "exact"
		} else if w.ResetAfterSeconds != nil && *w.ResetAfterSeconds >= 0 && !observed.IsZero() {
			reset = observed.UnixMilli() + int64(*w.ResetAfterSeconds*1000)
			accuracy = "estimated"
		}
		if w.UsedPercent == nil && reset == 0 {
			continue
		}
		id := "bps-primary"
		if i == 1 {
			id = "bps-secondary"
		}
		windows = append(windows, CodexInspectionQuotaWindow{ID: id, LabelKey: "官方账号用量", UsedPercent: w.UsedPercent, ResetLabel: "-", ResetAtMS: reset, ResetAccuracy: accuracy, LimitWindowSeconds: w.LimitWindowSeconds, ModelScope: &CodexInspectionQuotaModelScope{Kind: "feature", Key: "bps_account_usage", Complete: false}})
	}
	return windows
}
