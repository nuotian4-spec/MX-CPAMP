package kilo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) Probes(ctx context.Context, method, id, reference string, fields map[string]json.RawMessage) (model.KiloProbes, error) {
	var empty model.KiloProbes
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return empty, failure(400, "invalid_account", "账号编号无效")
	}
	if method != http.MethodGet && method != http.MethodPut {
		return empty, failure(405, "method_not_allowed", "请求方法未支持")
	}
	if method == http.MethodPut {
		if len(reference) != 32 {
			return empty, failure(400, "connection_reference_required", "请先读取当前探测配置")
		}
		if _, err := hex.DecodeString(reference); err != nil {
			return empty, failure(400, "connection_reference_required", "连接引用无效")
		}
		if !validProbeFields(fields) {
			return empty, failure(400, "invalid_probe_config", "请提交完整两类配置、非负版本和有效模型周期")
		}
	}
	setup, err := s.setup(ctx)
	if method == http.MethodGet {
		setup, reference, err = s.boundSetup(ctx)
	}
	if err != nil {
		return empty, err
	}
	guard, ok := s.connections.(connectionGuard)
	if !ok {
		return empty, failure(503, "connection_reference_unavailable", "连接引用校验暂不可用")
	}
	if guard.CheckConnection(ctx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "管理连接已变化，请重新读取")
	}
	result, requestErr := cpa.RequestKiloProbes(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, method, id, fields)
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if guard.CheckConnection(guardCtx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "管理连接已变化，原目标状态待核；请重新读取，勿直接重试")
	}
	if ctx.Err() != nil {
		return empty, failure(499, "probe_request_canceled", "本页请求已中止，服务器保存结果待核")
	}
	if requestErr != nil {
		status := 502
		var upstream *cpa.KiloWriteError
		if errors.As(requestErr, &upstream) {
			status = upstream.Status
		}
		messages := map[int]string{400: "探测配置未被接受", 404: "Kilo账号不存在或不属于此通道", 409: "配置版本、账号或连接已变化，请重新读取", 499: "本页请求已中止，服务器保存结果待核", 502: "探测配置或状态回执未确认", 503: "探测配置暂不可用，请重新读取确认"}
		return empty, failure(status, "probe_result_unconfirmed", messages[status])
	}
	result.ConnectionRef = reference
	return result, nil
}

func validProbeFields(fields map[string]json.RawMessage) bool {
	validJSON := func(raw json.RawMessage, target any) bool {
		return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" && json.Unmarshal(raw, target) == nil
	}
	var revision int64
	var health, probe map[string]json.RawMessage
	if len(fields) != 3 || !validJSON(fields["expected_revision"], &revision) || revision < 0 || !validJSON(fields["health"], &health) || !validJSON(fields["model_probe"], &probe) || len(health) != 1 || len(probe) != 3 {
		return false
	}
	var healthEnabled, probeEnabled bool
	var modelID string
	var interval int
	return validJSON(health["enabled"], &healthEnabled) && validJSON(probe["enabled"], &probeEnabled) && validJSON(probe["model"], &modelID) && (!probeEnabled || strings.TrimSpace(modelID) != "") && validJSON(probe["interval_seconds"], &interval) && interval >= 60 && interval <= 86400
}
