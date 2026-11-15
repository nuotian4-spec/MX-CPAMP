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

func (s *Service) Quota(ctx context.Context, action, id, reference string, fields map[string]json.RawMessage) (model.KiloQuotaResult, error) {
	var empty model.KiloQuotaResult
	read := action == "sources" || action == "get" || action == "policy_get"
	if action != "sources" && (id == "" || id == "." || id == ".." || len(id) > 1024 || strings.ContainsAny(id, "/\\")) {
		return empty, failure(400, "invalid_account", "账号编号无效")
	}
	if err := validateQuotaFields(action, fields); err != nil {
		return empty, err
	}
	setup, err := s.setup(ctx)
	if err != nil {
		return empty, err
	}
	if read {
		setup, reference, err = s.boundSetup(ctx)
		if err != nil {
			return empty, err
		}
	} else {
		if len(reference) != 32 {
			return empty, failure(400, "invalid_connection_reference", "请刷新账号列表")
		}
		if _, err := hex.DecodeString(reference); err != nil {
			return empty, failure(400, "invalid_connection_reference", "连接引用无效")
		}
	}
	guard, ok := s.connections.(connectionGuard)
	if !ok {
		return empty, failure(503, "connection_reference_unavailable", "连接引用暂不可用")
	}
	if guard.CheckConnection(ctx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "连接已变化，请刷新确认")
	}
	result, fetchErr := cpa.RequestKiloQuota(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, action, id, fields)
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if guard.CheckConnection(guardCtx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "连接已变化，额度结果待核，请刷新确认")
	}
	if ctx.Err() != nil && (result.View == nil || result.Status != 499) {
		return empty, failure(499, "quota_cancelled", "本地请求已中止，额度结果待核")
	}
	if fetchErr != nil {
		status := 502
		var upstream *cpa.KiloWriteError
		if errors.As(fetchErr, &upstream) {
			status = upstream.Status
		}
		messages := map[int]string{400: "额度请求字段无效", 401: "管理认证已失效", 403: "管理请求被拒绝", 404: "Kilo账号已不存在", 409: "账号来源、绑定版本或连接发生冲突，请刷新确认", 499: "额度请求已中止，结果待核", 502: "额度回执无效，结果未确认", 503: "额度管理暂不可用，写入或查询结果待核"}
		return empty, failure(status, "quota_unconfirmed", messages[status])
	}
	if result.Sources != nil {
		result.Sources.ConnectionRef = reference
	}
	if result.View != nil {
		result.View.ConnectionRef = reference
	}
	if result.Policy != nil {
		result.Policy.ConnectionRef = reference
	}
	return result, nil
}

func validateQuotaFields(action string, fields map[string]json.RawMessage) error {
	invalid := func() error { return failure(400, "invalid_quota_input", "额度来源字段或绑定版本无效") }
	if action == "sources" || action == "get" || action == "policy_get" {
		if len(fields) != 0 {
			return invalid()
		}
		return nil
	}
	if action == "policy_put" {
		if len(fields) < 3 || len(fields) > 4 {
			return invalid()
		}
		var revision uint64
		var enabled, confirm bool
		var interval int
		for key, value := range fields {
			if string(value) == "null" {
				return invalid()
			}
			switch key {
			case "expected_policy_revision":
				if json.Unmarshal(value, &revision) != nil || revision > 9007199254740991 {
					return invalid()
				}
			case "enabled":
				if json.Unmarshal(value, &enabled) != nil {
					return invalid()
				}
			case "interval_seconds":
				if json.Unmarshal(value, &interval) != nil || interval < 60 || interval > 86400 {
					return invalid()
				}
			case "confirm_scope":
				if json.Unmarshal(value, &confirm) != nil {
					return invalid()
				}
			default:
				return invalid()
			}
		}
		for _, key := range []string{"expected_policy_revision", "enabled", "interval_seconds"} {
			if _, ok := fields[key]; !ok {
				return invalid()
			}
		}
		return nil
	}
	if action != "put" && action != "delete" && action != "query" {
		return invalid()
	}
	var revision uint64
	raw, ok := fields["expected_binding_revision"]
	if !ok || string(raw) == "null" || json.Unmarshal(raw, &revision) != nil || revision > 9007199254740991 {
		return invalid()
	}
	if action != "put" {
		if len(fields) != 1 {
			return invalid()
		}
		return nil
	}
	if len(fields) != 2 {
		return invalid()
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(fields["source"], &source) != nil || source == nil {
		return invalid()
	}
	readString := func(key string) (string, bool) {
		var value string
		raw, ok := source[key]
		return value, ok && string(raw) != "null" && json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != ""
	}
	var kind string
	if json.Unmarshal(source["kind"], &kind) != nil {
		return invalid()
	}
	allowed := map[string]bool{"kind": true, "chatgpt_account_id": true}
	switch kind {
	case "cpa_codex":
		allowed["credential_id"] = true
		if len(source) != 3 {
			return invalid()
		}
		if _, ok := readString("credential_id"); !ok {
			return invalid()
		}
		if _, ok := readString("chatgpt_account_id"); !ok {
			return invalid()
		}
	case "imported":
		allowed["access_token"] = true
		if len(source) < 2 || len(source) > 3 {
			return invalid()
		}
		if _, ok := readString("access_token"); !ok {
			return invalid()
		}
		if _, exists := source["chatgpt_account_id"]; exists {
			if _, ok := readString("chatgpt_account_id"); !ok {
				return invalid()
			}
		}
	default:
		return invalid()
	}
	for key := range source {
		if !allowed[key] {
			return invalid()
		}
	}
	return nil
}
