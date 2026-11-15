package kilo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) TestModels(ctx context.Context) (model.KiloTestModels, error) {
	result, ref, err := readBound(ctx, s, cpa.FetchKiloTestModels)
	result.ConnectionRef = ref
	return result, err
}

func (s *Service) ManualTest(ctx context.Context, id, reference string, fields map[string]json.RawMessage) (model.KiloManualTest, error) {
	var empty model.KiloManualTest
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return empty, failure(400, "invalid_account", "账号编号无效")
	}
	if len(reference) != 32 {
		return empty, failure(400, "connection_reference_required", "请先刷新账号列表")
	}
	if _, err := hex.DecodeString(reference); err != nil {
		return empty, failure(400, "connection_reference_required", "连接引用无效")
	}
	var modelID, prompt string
	if len(fields) != 2 || json.Unmarshal(fields["model"], &modelID) != nil || strings.TrimSpace(modelID) == "" || json.Unmarshal(fields["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" {
		return empty, failure(400, "invalid_test_input", "请选择模型并填写非空文本提示词")
	}
	setup, err := s.setup(ctx)
	if err != nil {
		return empty, err
	}
	guard, ok := s.connections.(connectionGuard)
	if !ok {
		return empty, failure(503, "connection_reference_unavailable", "连接引用校验暂不可用")
	}
	if guard.CheckConnection(ctx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "连接已变化，请刷新账号后测试")
	}
	result, testErr := cpa.TestKiloAccount(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, id, modelID, prompt)
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if guard.CheckConnection(guardCtx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "连接已变化，原账号测试结果待核，不自动重试")
	}
	if ctx.Err() != nil {
		return empty, failure(499, "test_canceled", "本次请求已中止，服务端结果未确认")
	}
	if testErr != nil {
		status := 502
		var upstream *cpa.KiloWriteError
		if errors.As(testErr, &upstream) {
			status = upstream.Status
		}
		messages := map[int]string{400: "模型或提示词未被接受", 404: "Kilo账号不存在或不属于此通道", 409: "账号身份、执行状态或连接已变化，请刷新后测试", 429: "账号与出口准入等待预算耗尽", 499: "本次请求已中止，服务端结果未确认", 502: "手动测试回执或管理连接未确认", 503: "手动测试执行器或本地状态暂不可用"}
		return empty, failure(status, "manual_test_unconfirmed", messages[status])
	}
	result.AccountID = id
	result.ConnectionRef = reference
	return result, nil
}
