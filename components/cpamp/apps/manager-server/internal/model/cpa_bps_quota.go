package model

import (
	"fmt"
	"time"
)

// CPABPSQuota 绑定插件额度观测，不代表推理或自动恢复资格。
type CPABPSQuota struct {
	Provider  string          `json:"provider"`
	AuthIndex string          `json:"auth_index"`
	AuthID    string          `json:"auth_id"`
	Source    string          `json:"source"`
	QueriedAt string          `json:"queried_at"`
	Quota     CPABPSQuotaData `json:"quota"`
}
type CPABPSQuotaData struct {
	Subscription       *CPABPSSubscription `json:"subscription,omitempty"`
	Summary            []CPABPSQuotaMetric `json:"summary,omitempty"`
	ServerTimeOffsetMs int64               `json:"serverTimeOffsetMs,omitempty"`
	Groups             []CPABPSQuotaGroup  `json:"groups"`
}
type CPABPSSubscription struct {
	Plan     string `json:"plan,omitempty"`
	TierName string `json:"tierName,omitempty"`
	TierID   string `json:"tierId,omitempty"`
}
type CPABPSQuotaMetric struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Value    *float64 `json:"value"`
	Unit     string   `json:"unit,omitempty"`
	Format   string   `json:"format,omitempty"`
	Currency string   `json:"currency,omitempty"`
}
type CPABPSQuotaGroup struct {
	DisplayName string              `json:"displayName,omitempty"`
	Buckets     []CPABPSQuotaBucket `json:"buckets"`
}
type CPABPSQuotaBucket struct {
	Window            string   `json:"window,omitempty"`
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime,omitempty"`
	Description       string   `json:"description,omitempty"`
}

func (q CPABPSQuota) QuotaWindows() []CodexInspectionQuotaWindow {
	windows := []CodexInspectionQuotaWindow{}
	for gi, g := range q.Quota.Groups {
		for bi, b := range g.Buckets {
			var used *float64
			if b.RemainingFraction != nil {
				v := (1 - *b.RemainingFraction) * 100
				used = &v
			}
			label := b.Window
			if label == "" {
				label = g.DisplayName
			}
			if label == "" {
				label = "BPS 插件额度"
			}
			w := CodexInspectionQuotaWindow{ID: fmt.Sprintf("cpa-bps-%d-%d", gi, bi), LabelKey: label, UsedPercent: used, ResetLabel: "-", ResetAccuracy: "unknown", ModelScope: &CodexInspectionQuotaModelScope{Kind: "feature", Key: "cpa_bps_plugin_quota", Complete: false}}
			if at, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
				w.ResetAtMS = at.UnixMilli() - q.Quota.ServerTimeOffsetMs
				w.ResetAccuracy = "exact"
			}
			windows = append(windows, w)
		}
	}
	return windows
}
