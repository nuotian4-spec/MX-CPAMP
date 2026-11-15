package codexinspection

import (
	"context"
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func (s *Service) inspectSingleCPABPSAccount(ctx context.Context, setup store.Setup, item account, logger runLogger) model.CodexInspectionResult {
	base := resultFromAccount(item)
	base.Action = "keep"
	base.AutoRecoverEligible = false
	base.UsedPercent = nil
	base.State = "cpa_bps_quota_unknown"
	base.ActionReason = "BPS插件额度未知；仅观察，不执行账号动作"
	result, err := cpa.FetchCPABPSQuota(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, item.AuthIndex, item.RuntimeID)
	if s.managerConfigService != nil {
		_, current, resolveErr := s.resolveRuntime(ctx)
		if resolveErr != nil || current.CPAUpstreamURL != setup.CPAUpstreamURL || current.ManagementKey != setup.ManagementKey {
			base.ErrorKind = "cpa_target_changed"
			base.Error = "CPA连接已变更，额度结果已丢弃"
			return base
		}
	}
	if err != nil {
		base.ErrorKind = "cpa_bps_quota_failed"
		base.Error = "BPS插件额度未确认"
		return base
	}
	detail, _ := json.Marshal(map[string]any{"source": result.Source, "queried_at": result.QueriedAt, "provider": "cpa-bps"})
	base.ErrorDetail = string(detail)
	base.QuotaWindows = result.QuotaWindows()
	if result.Quota.Subscription != nil {
		base.PlanType = result.Quota.Subscription.Plan
	}
	for _, w := range base.QuotaWindows {
		if w.UsedPercent != nil && (base.UsedPercent == nil || *w.UsedPercent > *base.UsedPercent) {
			base.UsedPercent = w.UsedPercent
		}
	}
	if base.UsedPercent != nil {
		base.QuotaInventoryObserved = true
		base.State = "cpa_bps_quota_observed"
		base.ActionReason = "已观察BPS插件额度；推理未验证，保留账号"
	}
	logger.info(ctx, base.ActionReason, map[string]any{"provider": "cpa-bps", "authIndex": item.AuthIndex, "source": result.Source, "action": "keep"})
	return base
}
