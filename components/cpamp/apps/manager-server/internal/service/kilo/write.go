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
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type connectionGuard interface {
	CheckConnection(context.Context, string, store.Setup) error
}

func (s *Service) WriteAccount(ctx context.Context, method, id, reference string, fields map[string]json.RawMessage) (model.KiloAccountSaved, error) {
	var empty model.KiloAccountSaved
	if len(reference) != 32 {
		return empty, failure(400, "connection_reference_required", "请先刷新账号列表取得连接引用")
	}
	if _, err := hex.DecodeString(reference); err != nil {
		return empty, failure(400, "connection_reference_required", "连接引用格式无效，请刷新账号列表")
	}
	if method != http.MethodPost && method != http.MethodPatch && method != http.MethodDelete {
		return empty, failure(405, "method_not_allowed", "请求方法未支持")
	}
	if (method == http.MethodPost && id != "") || (method != http.MethodPost && (id == "" || strings.ContainsAny(id, "/\\") || id == "." || id == "..")) {
		return empty, failure(400, "invalid_account", "账号编号无效")
	}
	if err := validateAccountFields(method, fields); err != nil {
		return empty, err
	}
	setup, err := s.setup(ctx)
	if err != nil {
		return empty, err
	}
	guard, ok := s.connections.(connectionGuard)
	if !ok {
		return empty, failure(503, "connection_reference_unavailable", "连接引用校验暂不可用")
	}
	if err := guard.CheckConnection(ctx, reference, setup); err != nil {
		return empty, failure(409, "cpa_target_changed", "CPA连接或列表引用已变更，请刷新后操作")
	}
	account, writeErr := cpa.WriteKiloAccount(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, method, id, fields)
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := guard.CheckConnection(guardCtx, reference, setup); err != nil {
		return empty, failure(409, "result_unconfirmed", "CPA连接已变更，写入结果待核；请刷新确认，勿直接重试")
	}
	if ctx.Err() != nil {
		return empty, failure(499, "account_request_cancelled", "本地请求已中止，保存结果待核；请刷新确认")
	}
	if writeErr != nil {
		var upstream *cpa.KiloWriteError
		if errors.As(writeErr, &upstream) {
			switch upstream.Status {
			case 400:
				return empty, failure(400, "invalid_account_input", "账号字段未被接受，请检查输入")
			case 404:
				return empty, failure(404, "account_not_found", "账号已不存在，请刷新列表")
			case 403:
				return empty, failure(403, "organization_forbidden", "待用Key没有所选组织或个人空间权限，账号未保存")
			case 499:
				return empty, failure(499, "account_request_cancelled", "本次请求已中止，保存结果待核；请刷新确认")
			case 409:
				return empty, failure(409, "account_conflict", "账号状态冲突或有在途请求，请刷新确认")
			case 503:
				return empty, failure(503, "account_write_unavailable", "账号写入暂不可用，请刷新确认")
			}
		}
		return empty, failure(502, "result_unconfirmed", "账号写入结果待核，请刷新确认，勿直接重试")
	}
	return model.KiloAccountSaved{KiloAccount: account, ConnectionRef: reference}, nil
}

func validateAccountFields(method string, fields map[string]json.RawMessage) error {
	invalid := func() error {
		return failure(400, "invalid_account_input", "账号字段无效或本接口尚未支持该字段")
	}
	if method == http.MethodDelete {
		if len(fields) != 0 {
			return invalid()
		}
		return nil
	}
	if method == http.MethodPost {
		if _, ok := fields["api_key"]; !ok {
			return invalid()
		}
	}
	if len(fields) == 0 {
		return invalid()
	}
	for name, raw := range fields {
		value := strings.TrimSpace(string(raw))
		if value == "null" && name != "concurrency_limit" {
			return invalid()
		}
		switch name {
		case "organization_id":
			var organization string
			if method != http.MethodPatch || json.Unmarshal(raw, &organization) != nil || (organization != "" && !organizationIDPattern.MatchString(organization)) {
				return invalid()
			}
		case "api_key", "name":
			var text string
			if json.Unmarshal(raw, &text) != nil || (name == "api_key" && strings.TrimSpace(text) == "") {
				return invalid()
			}
		case "enabled", "allow_direct_when_proxy_full":
			var flag bool
			if json.Unmarshal(raw, &flag) != nil {
				return invalid()
			}
		case "priority":
			var priority int
			if json.Unmarshal(raw, &priority) != nil {
				return invalid()
			}
		case "concurrency_limit":
			if value == "null" {
				continue
			}
			var limit int64
			if json.Unmarshal(raw, &limit) != nil || limit < 1 || limit > 2147483647 {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return nil
}
