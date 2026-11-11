package codexinspection

import (
	"context"
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"strings"
	"time"
)

func (s *Service) inspectSingleBPSAccount(ctx context.Context, setup store.Setup, item account, logger runLogger) model.CodexInspectionResult {
	base := resultFromAccount(item)
	base.Action = "keep"
	base.State = "bps_access_unknown"
	base.AutoRecoverEligible = false
	base.UsedPercent = nil
	base.ActionReason = "BPS权限未确认；额度未知；推理未验证"
	result, err := cpa.FetchBPSAccess(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, item.AuthIndex)
	if err != nil {
		base.ErrorKind = "bps_access_failed"
		base.Error = "BPS权限查询未确认"
		return s.attachBPSUsage(ctx, setup, item, base)
	}
	base.StatusCode = result.UpstreamStatus
	base.State = "bps_access_unknown"
	if result.Error != nil {
		base.ErrorKind = *result.Error
		base.Error = "BPS权限查询失败：" + *result.Error
	} else if result.Allowed != nil && *result.Allowed {
		base.State = "bps_access_allowed"
		base.ActionReason = "BPS访问权限查询通过；额度未知；推理未验证"
	} else {
		base.State = "bps_access_restricted"
		base.ActionReason = "BPS访问权限受限；额度未知；推理未验证"
	}
	// Persist only the observation summary; model directories stay in page memory.
	summary := map[string]any{"allowed": result.Allowed, "queried_at": result.QueriedAt, "upstream_status": result.UpstreamStatus, "error": result.Error}
	raw, _ := json.Marshal(summary)
	base.ErrorDetail = string(raw)
	logger.info(ctx, base.ActionReason, map[string]any{"provider": "bps", "fileName": item.FileName, "allowed": result.Allowed, "queried_at": result.QueriedAt, "statusCode": result.UpstreamStatus, "errorKind": result.Error, "action": "keep"})
	return s.attachBPSUsage(ctx, setup, item, base)
}

func (s *Service) attachBPSUsage(ctx context.Context, setup store.Setup, item account, base model.CodexInspectionResult) model.CodexInspectionResult {
	usage, err := cpa.FetchBPSUsage(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, item.AuthIndex, "")
	detail := map[string]any{}
	_ = json.Unmarshal([]byte(base.ErrorDetail), &detail)
	if detail == nil {
		detail = map[string]any{}
	}
	if err != nil {
		detail["usage_error"] = "bps_usage_failed"
		base.ActionReason += "；官方账号用量未知"
	} else {
		detail["usage_source"] = usage.Source
		detail["usage_queried_at"] = usage.QueriedAt
		detail["usage_error"] = usage.Error
		detail["usage_upstream_status"] = usage.UpstreamStatus
		base.QuotaWindows = usage.QuotaWindows()
		if usage.Error == nil && len(base.QuotaWindows) > 0 {
			base.QuotaInventoryObserved = true
			if usage.PlanType != nil {
				base.PlanType = *usage.PlanType
			}
			if at, e := time.Parse(time.RFC3339, usage.QueriedAt); e == nil {
				base.CreatedAtMS = at.UnixMilli()
			}
			for _, w := range base.QuotaWindows {
				if w.UsedPercent != nil && (base.UsedPercent == nil || *w.UsedPercent > *base.UsedPercent) {
					base.UsedPercent = w.UsedPercent
				}
			}
			base.ActionReason = strings.ReplaceAll(base.ActionReason, "；额度未知", "") + "；官方账号用量已查询"
		} else {
			base.ActionReason += "；官方账号用量未知"
		}
	}
	raw, _ := json.Marshal(detail)
	base.ErrorDetail = string(raw)
	return base
}
