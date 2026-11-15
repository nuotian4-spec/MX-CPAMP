package kilo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) Device(ctx context.Context, action, id, reference string, fields map[string]json.RawMessage) (any, error) {
	if len(reference) != 32 {
		return nil, failure(400, "connection_reference_required", "请刷新账号列表取得连接引用")
	}
	if _, err := hex.DecodeString(reference); err != nil {
		return nil, failure(400, "connection_reference_required", "连接引用格式无效")
	}
	if action != "start" && (id == "" || id == "." || id == ".." || len(id) > 256 || strings.ContainsAny(id, "/\\")) {
		return nil, failure(400, "invalid_device_session", "授权会话编号无效")
	}
	if err := validateDeviceFields(action, fields); err != nil {
		return nil, err
	}
	setup, err := s.setup(ctx)
	if err != nil {
		return nil, err
	}
	guard, ok := s.connections.(connectionGuard)
	if !ok {
		return nil, failure(503, "connection_reference_unavailable", "连接引用校验暂不可用")
	}
	if guard.CheckConnection(ctx, reference, setup) != nil {
		return nil, failure(409, "cpa_target_changed", "授权连接已变更，请回到原连接核实会话")
	}
	result, writeErr := cpa.RequestKiloDevice(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, action, id, fields)
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if guard.CheckConnection(guardCtx, reference, setup) != nil {
		return nil, failure(409, "cpa_target_changed", "授权连接已变更，本次结果待核；请回到原连接核实，勿重复发起")
	}
	if ctx.Err() != nil {
		return nil, failure(499, "device_request_cancelled", "本次HTTP请求已中止，服务端授权状态待核；这不等于取消授权")
	}
	if writeErr != nil {
		if errors.Is(writeErr, cpa.ErrKiloDeviceEgressUnavailable) {
			return nil, failure(503, "device_egress_unavailable", "设备授权尚未启动：所选授权出口暂不可用，请检查代理健康、容量或出口配置")
		}
		var up *cpa.KiloWriteError
		status := 502
		if errors.As(writeErr, &up) {
			status = up.Status
		}
		if action == "start" && (status == 502 || status == 503) {
			return nil, failure(status, "device_start_unconfirmed", "设备授权启动结果未确认，请先核实管理连接与授权出口；尚未进入账号保存步骤")
		}
		messages := map[int]string{400: "授权字段未被接受，请检查输入", 403: "管理请求被拒绝，请检查当前管理连接", 404: "授权会话不存在或已回收，请核实账号列表", 409: "授权状态冲突或提交中，请核实同一会话状态", 429: "授权查询暂受限，原期限保持不变，请稍后显式核实", 499: "本次HTTP请求已中止，服务端授权状态待核；这不等于取消授权", 502: "授权上游结果未确认，请核实同一会话状态", 503: "授权操作暂不可用，保存结果待核；请先核实同一会话或账号列表"}
		return nil, failure(status, "device_result_unconfirmed", messages[status])
	}
	if result.Started != nil {
		result.Started.ConnectionRef = reference
		return result.Started, nil
	}
	if result.Saved != nil {
		result.Saved.ConnectionRef = reference
		return result.Saved, nil
	}
	result.State.ConnectionRef = reference
	return result.State, nil
}

func validateDeviceFields(action string, fields map[string]json.RawMessage) error {
	invalid := func() error { return failure(400, "invalid_device_input", "设备授权字段无效") }
	if action != "start" {
		if action != "poll" && action != "save" && action != "cancel" || len(fields) != 0 {
			return invalid()
		}
		return nil
	}
	account := map[string]json.RawMessage{}
	for k, v := range fields {
		if k == "authorization_egress" || k == "inference_egress" {
			continue
		}
		switch k {
		case "name", "priority", "enabled", "concurrency_limit", "allow_direct_when_proxy_full":
			account[k] = v
		default:
			return invalid()
		}
	}
	var name string
	if json.Unmarshal(fields["name"], &name) != nil || utf8.RuneCountInString(strings.TrimSpace(name)) < 1 || utf8.RuneCountInString(strings.TrimSpace(name)) > 256 {
		return invalid()
	}
	if validateAccountFields(http.MethodPatch, account) != nil {
		return invalid()
	}
	var egress map[string]json.RawMessage
	var mode string
	if json.Unmarshal(fields["authorization_egress"], &egress) != nil || json.Unmarshal(egress["mode"], &mode) != nil || (mode != "direct" && mode != "pool") {
		return invalid()
	}
	for key, raw := range egress {
		switch key {
		case "mode":
		case "proxy_id":
			var proxy string
			if json.Unmarshal(raw, &proxy) != nil || strings.TrimSpace(proxy) == "" || mode == "direct" {
				return invalid()
			}
		case "expected_revision":
			var revision uint64
			if string(raw) == "null" || json.Unmarshal(raw, &revision) != nil {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	if raw, ok := fields["inference_egress"]; ok {
		var draft map[string]json.RawMessage
		if json.Unmarshal(raw, &draft) != nil || draft == nil {
			return invalid()
		}
		if _, err := validateProxyFields(http.MethodPut, "egress", "device-draft", draft); err != nil {
			return invalid()
		}
		var candidates []string
		var proxy string
		_ = json.Unmarshal(draft["candidate_proxy_ids"], &candidates)
		_ = json.Unmarshal(egress["proxy_id"], &proxy)
		if len(candidates) > 0 && mode == "pool" {
			found := false
			for _, id := range candidates {
				if id == proxy {
					found = true
				}
			}
			if !found {
				return invalid()
			}
		}
	}
	return nil
}
