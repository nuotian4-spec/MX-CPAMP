package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) GetBPSResetCredits(ctx context.Context, authIndex, authID, reference string) (model.BPSResetCredits, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return model.BPSResetCredits{}, err
	}
	if err = s.checkConnectionRef(reference, setup); err != nil {
		return model.BPSResetCredits{}, err
	}
	result, err := cpa.FetchBPSResetCredits(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, authIndex, authID)
	if err != nil {
		return model.BPSResetCredits{}, failure(502, "bps_reset_credits_failed", "重置次数查询未确认")
	}
	current, err := s.setup(ctx)
	if err != nil {
		return model.BPSResetCredits{}, err
	}
	if !sameConnection(current, setup) {
		return model.BPSResetCredits{}, failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
	}
	return result, nil
}
