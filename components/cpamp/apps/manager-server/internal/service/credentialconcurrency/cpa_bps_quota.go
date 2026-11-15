package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) GetCPABPSQuota(ctx context.Context, authIndex, authID, reference string) (model.CPABPSQuota, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return model.CPABPSQuota{}, err
	}
	if err = s.checkConnectionRef(reference, setup); err != nil {
		return model.CPABPSQuota{}, err
	}
	result, fetchErr := cpa.FetchCPABPSQuota(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, authIndex, authID)
	current, err := s.setup(ctx)
	if err != nil {
		return model.CPABPSQuota{}, err
	}
	if !sameConnection(current, setup) {
		return model.CPABPSQuota{}, failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
	}
	if fetchErr != nil {
		return model.CPABPSQuota{}, failure(502, "cpa_bps_quota_failed", "BPS插件额度未确认")
	}
	return result, nil
}
