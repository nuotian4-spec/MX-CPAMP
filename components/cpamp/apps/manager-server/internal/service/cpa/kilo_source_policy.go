package cpa

import (
	"slices"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func validKiloSourcePolicy(p *model.KiloSourcePolicy) bool {
	if p == nil || p.IntervalSeconds < 60 || p.IntervalSeconds > 86400 || p.Revision > 9007199254740991 || p.PauseGeneration > 9007199254740991 {
		return false
	}
	if !slices.Contains([]string{"", "18000+604800", "604800", "2592000"}, p.Scope) || !slices.Contains([]string{"disabled", "pending", "ready", "paused", "source_pending", "query_failed", "waiting_query_egress", "storage_failed"}, p.Status) {
		return false
	}
	for _, value := range []*string{p.ObservedAt, p.NextQueryAt} {
		if value != nil {
			if _, err := time.Parse(time.RFC3339, *value); err != nil {
				return false
			}
		}
	}
	if p.ErrorCode != nil && !slices.Contains(quotaErrorCodes, *p.ErrorCode) && *p.ErrorCode != "query_egress_unavailable" && *p.ErrorCode != "storage_failed" {
		return false
	}
	p.ConnectionRef = ""
	return true
}
