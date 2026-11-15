package kilo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

func (s *Service) Proxies(ctx context.Context) (model.KiloProxyState, error) {
	return s.readProxies(ctx, "proxies")
}

func (s *Service) ProxyRecycle(ctx context.Context) (model.KiloProxyState, error) {
	return s.readProxies(ctx, "proxy-recycle")
}

func (s *Service) readProxies(ctx context.Context, resource string) (model.KiloProxyState, error) {
	setup, ref, err := s.boundSetup(ctx)
	if err != nil {
		return model.KiloProxyState{}, err
	}
	result, readErr := cpa.RequestKiloProxies(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, http.MethodGet, resource, nil)
	guard, ok := s.connections.(connectionGuard)
	if !ok || guard.CheckConnection(ctx, ref, setup) != nil {
		return model.KiloProxyState{}, failure(409, "cpa_target_changed", "CPA连接或引用已变更，请重新读取代理列表")
	}
	if readErr != nil {
		status := 502
		var upstream *cpa.KiloWriteError
		if errors.As(readErr, &upstream) {
			status = upstream.Status
		}
		return model.KiloProxyState{}, failure(status, "proxy_read_failed", "代理状态读取未确认，请刷新核对")
	}
	result.ConnectionRef = ref
	return result, nil
}

func (s *Service) WriteProxies(ctx context.Context, method, kind, id, reference string, fields map[string]json.RawMessage) (model.KiloProxyState, error) {
	var empty model.KiloProxyState
	if len(reference) != 32 {
		return empty, failure(400, "connection_reference_required", "请先刷新代理列表取得连接引用")
	}
	if _, err := hex.DecodeString(reference); err != nil {
		return empty, failure(400, "connection_reference_required", "连接引用格式无效")
	}
	resource, err := validateProxyFields(method, kind, id, fields)
	if err != nil {
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
	if guard.CheckConnection(ctx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "CPA连接或列表引用已变更，请刷新核对")
	}
	result, writeErr := cpa.RequestKiloProxies(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, method, resource, fields)
	// 请求取消后仍独立核连接，避免把取消误报成连接漂移。
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if guard.CheckConnection(guardCtx, reference, setup) != nil {
		return empty, failure(409, "result_unconfirmed", "CPA连接已变更，写入结果待核；请刷新确认，勿直接重试")
	}
	if ctx.Err() != nil {
		return empty, failure(499, "proxy_operation_cancelled", "代理操作已取消，未确认完成；请刷新核对")
	}
	if writeErr != nil {
		var duplicate *cpa.KiloProxyDuplicateError
		if errors.As(writeErr, &duplicate) {
			return empty, &ProxyIPConflict{IP: duplicate.IP, ProxyID: duplicate.ProxyID}
		}
		var up *cpa.KiloWriteError
		if errors.As(writeErr, &up) {
			switch up.Status {
			case 400:
				return empty, failure(400, "invalid_proxy_input", "代理或出口字段未被接受，请检查输入")
			case 404:
				return empty, failure(404, "proxy_not_found", "代理或账号已不存在，请刷新核对")
			case 409:
				return empty, failure(409, "proxy_conflict", "版本已变化或资源有在途请求，请刷新核对后显式提交")
			case 499:
				return empty, failure(499, "proxy_operation_cancelled", "代理操作已取消，未确认完成；请刷新核对")
			case 502:
				if kind == "proxy-restore" {
					return empty, failure(502, "proxy_probe_failed", "恢复健康检测或结果未确认；恢复失败会保留回收站，请刷新核对")
				}
				if kind == "proxy-recheck" {
					return empty, failure(502, "proxy_probe_failed", "强制健康重验或结果未确认，请刷新核对健康状态")
				}
			case 503:
				return empty, failure(503, "proxy_write_unavailable", "代理管理暂不可用，写入结果请刷新确认")
			}
		}
		return empty, failure(502, "result_unconfirmed", "代理写入结果待核，请刷新确认，勿直接重试")
	}
	result.ConnectionRef = reference
	return result, nil
}

func validateProxyFields(method, kind, id string, fields map[string]json.RawMessage) (string, error) {
	invalid := func() (string, error) { return "", failure(400, "invalid_proxy_input", "代理或出口字段无效") }
	var revision uint64
	if raw, ok := fields["expected_revision"]; !ok || string(raw) == "null" || json.Unmarshal(raw, &revision) != nil {
		return invalid()
	}
	validID := id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\")
	resource := ""
	switch {
	case kind == "proxies" && method == http.MethodPost && id == "":
		resource = "proxies"
	case kind == "proxies" && method == http.MethodPatch && validID:
		resource = "proxies/" + url.PathEscape(id)
	case kind == "proxies" && method == http.MethodDelete && validID:
		resource = "proxies/" + url.PathEscape(id)
	case kind == "proxy-restore" && method == http.MethodPost && validID:
		resource = "proxy-recycle/" + url.PathEscape(id) + "/restore"
	case kind == "proxy-recheck" && method == http.MethodPost && validID:
		resource = "proxies/" + url.PathEscape(id) + "/recheck"
	case kind == "proxy-recycle" && method == http.MethodDelete && id == "":
		resource = "proxy-recycle"
	case kind == "proxy-pool" && method == http.MethodPatch && id == "":
		resource = "proxy-pool"
	case kind == "egress" && method == http.MethodPut && validID:
		resource = "accounts/" + url.PathEscape(id) + "/egress"
	default:
		return invalid()
	}
	for name, raw := range fields {
		if strings.TrimSpace(string(raw)) == "null" && !(kind == "egress" && name == "candidate_proxy_ids") && !(kind == "proxy-pool" && name == "ip_entry_limit") {
			return invalid()
		}
		if name == "expected_revision" {
			continue
		}
		allowed := kind == "proxy-recycle" && name == "confirm" || kind == "proxies" && method != http.MethodDelete && (name == "url" || name == "name" || name == "enabled" || name == "account_id" || ((name == "order" || name == "fixed_exit_ip") && method == http.MethodPatch)) || kind == "proxy-pool" && (name == "proxy_limit" || name == "auto_assignment_enabled" || name == "max_unique_accounts" || name == "ip_concurrency_limit" || name == "ip_entry_limit") || kind == "egress" && (name == "mode" || name == "exclusive_proxy_ids" || name == "candidate_proxy_ids" || name == "primary_proxy_id" || name == "capacity_mode" || name == "allocation_mode" || name == "max_ips" || name == "specified_ips")
		if !allowed {
			return invalid()
		}
		switch name {
		case "confirm":
			var v bool
			if json.Unmarshal(raw, &v) != nil || !v {
				return invalid()
			}
		case "enabled", "auto_assignment_enabled":
			var v bool
			if json.Unmarshal(raw, &v) != nil {
				return invalid()
			}
		case "order":
			var v int
			if json.Unmarshal(raw, &v) != nil {
				return invalid()
			}
		case "ip_entry_limit":
			if strings.TrimSpace(string(raw)) == "null" {
				continue
			}
			fallthrough
		case "proxy_limit", "ip_concurrency_limit":
			var v int
			if json.Unmarshal(raw, &v) != nil || v < 1 || v > 100 {
				return invalid()
			}
		case "max_unique_accounts", "max_ips":
			var v int64
			if json.Unmarshal(raw, &v) != nil || v < 1 || v > 9007199254740991 {
				return invalid()
			}
		case "fixed_exit_ip":
			var v string
			if json.Unmarshal(raw, &v) != nil || (v != "" && net.ParseIP(v) == nil) {
				return invalid()
			}
		case "exclusive_proxy_ids", "candidate_proxy_ids", "specified_ips":
			var v []string
			if json.Unmarshal(raw, &v) != nil {
				return invalid()
			}
			for _, id := range v {
				if id == "" || name == "specified_ips" && net.ParseIP(id) == nil {
					return invalid()
				}
			}
		default:
			var v string
			if json.Unmarshal(raw, &v) != nil || (name == "url" && strings.TrimSpace(v) == "") {
				return invalid()
			}
		}
	}
	if kind == "proxies" && method == http.MethodPost {
		if _, ok := fields["url"]; !ok {
			return invalid()
		}
	}
	if kind == "proxy-recycle" {
		if _, ok := fields["confirm"]; !ok {
			return invalid()
		}
	}
	if kind == "proxy-pool" {
		if len(fields) < 2 {
			return invalid()
		}
	}
	if kind == "egress" {
		for _, key := range []string{"mode", "exclusive_proxy_ids", "primary_proxy_id"} {
			if _, ok := fields[key]; !ok {
				return invalid()
			}
		}
		var mode, primary string
		var ids []string
		_ = json.Unmarshal(fields["mode"], &mode)
		_ = json.Unmarshal(fields["primary_proxy_id"], &primary)
		_ = json.Unmarshal(fields["exclusive_proxy_ids"], &ids)
		var candidates []string
		if raw, ok := fields["candidate_proxy_ids"]; ok && strings.TrimSpace(string(raw)) != "null" {
			if json.Unmarshal(raw, &candidates) != nil || len(candidates) == 0 {
				return invalid()
			}
			contains := func(id string) bool {
				for _, candidate := range candidates {
					if candidate == id {
						return true
					}
				}
				return false
			}
			if primary != "" && !contains(primary) {
				return invalid()
			}
			for _, id := range ids {
				if !contains(id) {
					return invalid()
				}
			}
		}
		if mode != "pool" && mode != "direct" && mode != "auto" || (mode == "direct" || mode == "auto") && (len(ids) > 0 || primary != "" || len(candidates) > 0) {
			return invalid()
		}
	}
	if kind == "egress" {
		var capacity, allocation, mode string
		var maxIPs int64 = 1
		var specified []string
		_ = json.Unmarshal(fields["capacity_mode"], &capacity)
		_ = json.Unmarshal(fields["allocation_mode"], &allocation)
		_ = json.Unmarshal(fields["mode"], &mode)
		if raw, ok := fields["max_ips"]; ok {
			_ = json.Unmarshal(raw, &maxIPs)
		}
		_ = json.Unmarshal(fields["specified_ips"], &specified)
		if capacity != "" && capacity != "fixed" && capacity != "ip" || allocation != "" && allocation != "auto" && allocation != "specified" || capacity == "ip" && mode != "auto" || allocation == "specified" && (capacity != "ip" || len(specified) == 0 || int64(len(specified)) > maxIPs) {
			return invalid()
		}
	}
	return resource, nil
}

// ProxyIPConflict与未知409分开，客户端只对确定拒绝继续下一行。
type ProxyIPConflict struct{ IP, ProxyID string }

func (e *ProxyIPConflict) Error() string { return "proxy_ip_already_registered" }
func (e *ProxyIPConflict) Unwrap() error {
	return failure(409, "proxy_ip_already_registered", "公网IP已登记，重复条目未保存")
}

func (s *Service) PreviewProxy(ctx context.Context, reference string, fields map[string]json.RawMessage) (model.KiloProxyPreview, error) {
	var empty model.KiloProxyPreview
	var proxyURL string
	if len(fields) != 1 || json.Unmarshal(fields["url"], &proxyURL) != nil || strings.TrimSpace(proxyURL) == "" {
		return empty, failure(400, "invalid_proxy_input", "预览仅接受代理URL")
	}
	if len(reference) != 32 {
		return empty, failure(400, "connection_reference_required", "请先刷新代理列表取得连接引用")
	}
	if _, err := hex.DecodeString(reference); err != nil {
		return empty, failure(400, "connection_reference_required", "连接引用格式无效")
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
		return empty, failure(409, "cpa_target_changed", "CPA连接已变化，请重新读取代理列表")
	}
	result, previewErr := cpa.PreviewKiloProxy(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey, fields)
	guardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if guard.CheckConnection(guardCtx, reference, setup) != nil {
		return empty, failure(409, "cpa_target_changed", "CPA连接已变化，预览已废弃")
	}
	if ctx.Err() != nil {
		return empty, failure(499, "proxy_operation_cancelled", "预览已取消")
	}
	if previewErr != nil {
		status := 502
		var up *cpa.KiloWriteError
		if errors.As(previewErr, &up) {
			status = up.Status
		}
		return empty, failure(status, "proxy_preview_failed", "代理预览未确认，请重新读取状态")
	}
	return result, nil
}
