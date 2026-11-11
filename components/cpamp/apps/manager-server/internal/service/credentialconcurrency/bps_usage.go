package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) GetBPSUsage(ctx context.Context, authIndex, authID, reference string) (model.BPSUsage, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return model.BPSUsage{}, err
	}
	if err = s.checkConnectionRef(reference, setup); err != nil {
		return model.BPSUsage{}, err
	}
	result, err := cpa.FetchBPSUsage(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, authIndex, authID)
	if err != nil {
		return model.BPSUsage{}, failure(502, "bps_usage_failed", "官方账号用量查询未确认")
	}
	current, err := s.setup(ctx)
	if err != nil {
		return model.BPSUsage{}, err
	}
	if !sameConnection(current, setup) {
		return model.BPSUsage{}, failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
	}
	return result, nil
}
