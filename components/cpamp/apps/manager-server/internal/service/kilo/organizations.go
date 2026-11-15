package kilo

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

var organizationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s *Service) Organizations(ctx context.Context, id string) (model.KiloOrganizations, error) {
	var empty model.KiloOrganizations
	if id == "" || id == "." || id == ".." || len(id) > 256 || strings.ContainsAny(id, "/\\") {
		return empty, failure(400, "invalid_account", "账号编号无效")
	}
	setup, ref, err := s.boundSetup(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return empty, failure(499, "organizations_cancelled", "组织查询已中止")
		}
		return empty, err
	}
	result, fetchErr := cpa.FetchKiloOrganizations(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, id)
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	after, err := s.setup(guardCtx)
	if err != nil || !sameConnection(setup, after) {
		return empty, failure(409, "cpa_target_changed", "组织查询期间连接已变更，请刷新账号列表")
	}
	if ctx.Err() != nil {
		return empty, failure(499, "organizations_cancelled", "组织查询已中止，账号未被本次查询修改")
	}
	if fetchErr != nil {
		status := 502
		var upstream *cpa.KiloWriteError
		if errors.As(fetchErr, &upstream) {
			status = upstream.Status
		}
		messages := map[int]string{400: "组织查询参数未被接受", 403: "当前账号Key没有组织查询权限", 404: "账号已不存在，请刷新列表", 409: "查询期间账号身份或连接已变更，请刷新确认", 499: "组织查询已中止，账号未被本次查询修改", 502: "组织查询回执无效，未取得可用列表", 503: "组织查询暂不可用，请稍后显式重试"}
		return empty, failure(status, "organizations_unconfirmed", messages[status])
	}
	result.ConnectionRef = ref
	return result, nil
}
