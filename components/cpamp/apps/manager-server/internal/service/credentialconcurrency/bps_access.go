package credentialconcurrency

import (
	"context"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) GetBPSAccess(ctx context.Context, authIndex, reference string) (model.BPSAccess, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return model.BPSAccess{}, err
	}
	if err = s.checkConnectionRef(reference, setup); err != nil {
		return model.BPSAccess{}, err
	}
	result, err := cpa.FetchBPSAccess(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, authIndex)
	if err != nil {
		return model.BPSAccess{}, failure(502, "bps_access_failed", "BPS权限查询未确认")
	}
	current, err := s.setup(ctx)
	if err != nil {
		return model.BPSAccess{}, err
	}
	if !sameConnection(current, setup) {
		return model.BPSAccess{}, failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
	}
	return result, nil
}
