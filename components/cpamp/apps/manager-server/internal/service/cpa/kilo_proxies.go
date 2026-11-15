package cpa

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func FetchKiloProxies(ctx context.Context, client *http.Client, baseURL, key string) (model.KiloProxyState, error) {
	return RequestKiloProxies(ctx, client, baseURL, key, http.MethodGet, "proxies", nil)
}

// RequestKiloProxies仅调用固定管理资源一次，投影丢弃URL及上游额外字段。
func RequestKiloProxies(ctx context.Context, client *http.Client, baseURL, key, method, resource string, fields map[string]json.RawMessage) (model.KiloProxyState, error) {
	var result model.KiloProxyState
	raw, err := requestKiloProxyJSON(ctx, client, baseURL, key, method, resource, fields)
	if err != nil {
		return result, err
	}
	if err != nil || len(raw) > 1024*1024 || json.Unmarshal(raw, &result) != nil || result.ProxyLimit < 1 || result.ProxyLimit > 100 || !validKiloAutoState(&result) {
		return model.KiloProxyState{}, &KiloWriteError{Status: 502}
	}
	for i := range result.Proxies {
		p := &result.Proxies[i]
		if !validProxyAdmission(&p.KiloProxyAdmission) {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
		if p.FixedExitIP != "" && net.ParseIP(p.FixedExitIP) == nil {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
		if p.ObservedExitIP != nil && net.ParseIP(*p.ObservedExitIP) == nil || !quotaTime(p.ObservedAt) || !slices.Contains([]string{"", "unknown", "pending", "ok", "failed", "stale"}, p.ObservationStatus) || !slices.Contains([]string{"", "none", "account", "device"}, p.OwnerKind) {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
		if p.ID == "" || p.Host == "" || strings.ContainsAny(p.Host, "@/\\?#") || p.Active < 0 {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
		if p.Port != "" {
			port, err := strconv.Atoi(p.Port)
			if err != nil || port < 1 || port > 65535 {
				return model.KiloProxyState{}, &KiloWriteError{Status: 502}
			}
		}
	}
	for i := range result.Recycle {
		p := &result.Recycle[i]
		if !validProxyAdmission(&p.KiloProxyAdmission) {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
		if (p.FixedExitIP != "" && net.ParseIP(p.FixedExitIP) == nil) || !slices.Contains(kiloRetirementReasons, p.RetirementReason) {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
		at, err := time.Parse(time.RFC3339Nano, p.ArchivedAt)
		_, offset := at.Zone()
		if p.ID == "" || p.Host == "" || strings.ContainsAny(p.Host, "@/\\?#") || err != nil || offset != 0 {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
		if p.Port != "" {
			port, err := strconv.Atoi(p.Port)
			if err != nil || port < 1 || port > 65535 {
				return model.KiloProxyState{}, &KiloWriteError{Status: 502}
			}
		}
	}
	if result.Recycle == nil {
		result.Recycle = []model.KiloRecycledProxy{}
	}
	for _, a := range result.Accounts {
		if a.AccountID == "" || (a.Mode != "pool" && a.Mode != "direct" && a.Mode != "auto") || (a.AssignedIP != "" && net.ParseIP(a.AssignedIP) == nil) || !slices.Contains([]string{"", "manual", "waiting", "assigned", "draining", "egress_unavailable"}, a.AllocationState) || (!slices.Contains(kiloRetirementReasons, a.AllocationReason) && a.AllocationReason != "storage_failed" && a.AllocationReason != "specified_conflict" && a.AllocationReason != "capacity_wait" && a.AllocationReason != "partial_egress_unavailable") {
			return model.KiloProxyState{}, &KiloWriteError{Status: 502}
		}
	}
	if result.Proxies == nil {
		result.Proxies = []model.KiloProxy{}
	}
	if result.Accounts == nil {
		result.Accounts = []model.KiloAccountEgress{}
	}
	result.ConnectionRef = ""
	return result, nil
}

// KiloProxyDuplicateError只承载明确重复拒绝与通过白名单的定位字段。
type KiloProxyDuplicateError struct{ IP, ProxyID string }

func (e *KiloProxyDuplicateError) Error() string { return "proxy_ip_already_registered" }
func (e *KiloProxyDuplicateError) Unwrap() error { return &KiloWriteError{Status: 409} }

func requestKiloProxyJSON(ctx context.Context, client *http.Client, baseURL, key, method, resource string, fields map[string]json.RawMessage) ([]byte, error) {
	var body io.Reader
	if fields != nil {
		raw, err := json.Marshal(fields)
		if err != nil {
			return nil, &KiloWriteError{Status: 400}
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, NormalizeBaseURL(baseURL)+"/v8/management/kilo/"+resource, body)
	if err != nil {
		return nil, &KiloWriteError{Status: 502}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	local := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		local = *client
	}
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := local.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &KiloWriteError{Status: 499}
		}
		return nil, &KiloWriteError{Status: 502}
	}
	defer res.Body.Close()
	want := http.StatusOK
	if method == http.MethodPost && resource == "proxies" {
		want = http.StatusCreated
	}
	if res.StatusCode != want {
		status := res.StatusCode
		switch status {
		case 400, 404, 409, 499, 503:
		default:
			status = 502
		}
		if status == http.StatusConflict {
			raw, readErr := io.ReadAll(io.LimitReader(res.Body, 4097))
			var body struct {
				Error   string `json:"error"`
				IP      string `json:"ip"`
				ProxyID string `json:"proxy_id"`
			}
			if readErr == nil && len(raw) <= 4096 && json.Unmarshal(raw, &body) == nil && body.Error == "proxy_ip_already_registered" {
				duplicate := &KiloProxyDuplicateError{}
				if validPublicProxyIP(body.IP) {
					duplicate.IP = body.IP
				}
				if safeProxyReference(body.ProxyID) {
					duplicate.ProxyID = body.ProxyID
				}
				return nil, duplicate
			}
		}
		return nil, &KiloWriteError{Status: status}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if ctx.Err() != nil {
		return nil, &KiloWriteError{Status: 499}
	}

	if err != nil || len(raw) > 1024*1024 {
		return nil, &KiloWriteError{Status: 502}
	}
	return raw, nil
}

var proxyAdmissionReasons = []string{"", "unverified", "probe_failed", "ip_unknown", "ip_changed", "not_public_ip", "duplicate_ip"}

func validPublicProxyIP(raw string) bool {
	ip, err := netip.ParseAddr(raw)
	return err == nil && ip.Unmap().String() == raw && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}
func safeProxyReference(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}
func validProxyAdmission(p *model.KiloProxyAdmission) bool {
	if p.AdmissionStatus == "" {
		p.AdmissionStatus = "legacy"
	}
	if !slices.Contains([]string{"legacy", "pending", "admitted"}, p.AdmissionStatus) || !slices.Contains(proxyAdmissionReasons, p.AdmissionReason) || !quotaTime(p.AdmittedAt) {
		return false
	}
	if p.AdmittedIP != "" && !validPublicProxyIP(p.AdmittedIP) {
		return false
	}
	if p.AdmissionStatus != "pending" && p.AdmissionReason != "" {
		return false
	}
	if p.AdmissionStatus == "legacy" && (p.AdmittedIP != "" || p.AdmittedAt != nil) {
		return false
	}
	if p.AdmittedIP == "" && p.AdmittedAt != nil {
		return false
	}
	return p.AdmissionStatus != "admitted" || p.AdmittedIP != "" && p.AdmittedAt != nil
}

func PreviewKiloProxy(ctx context.Context, client *http.Client, baseURL, key string, fields map[string]json.RawMessage) (model.KiloProxyPreview, error) {
	var result model.KiloProxyPreview
	raw, err := requestKiloProxyJSON(ctx, client, baseURL, key, http.MethodPost, "proxies/preview", fields)
	if err != nil {
		return result, err
	}
	var fieldsPresent map[string]json.RawMessage
	if json.Unmarshal(raw, &fieldsPresent) != nil || json.Unmarshal(raw, &result) != nil {
		return result, &KiloWriteError{Status: 502}
	}
	for _, key := range []string{"status", "reason", "observed_exit_ip", "observed_at"} {
		if value, ok := fieldsPresent[key]; !ok || (key == "status" || key == "reason") && string(value) == "null" {
			return model.KiloProxyPreview{}, &KiloWriteError{Status: 502}
		}
	}
	if !slices.Contains([]string{"ready", "pending", "duplicate"}, result.Status) || !slices.Contains(proxyAdmissionReasons, result.Reason) || !quotaTime(result.ObservedAt) || result.ObservedExitIP != nil && !validPublicProxyIP(*result.ObservedExitIP) || result.RegisteredProxyID != "" && !safeProxyReference(result.RegisteredProxyID) {
		return model.KiloProxyPreview{}, &KiloWriteError{Status: 502}
	}
	if result.Status == "ready" && (result.Reason != "" || result.ObservedExitIP == nil || result.ObservedAt == nil || result.RegisteredProxyID != "") || result.Status == "duplicate" && (result.Reason != "duplicate_ip" || result.ObservedExitIP == nil || result.ObservedAt == nil) || result.Status == "pending" && result.Reason == "" {
		return model.KiloProxyPreview{}, &KiloWriteError{Status: 502}
	}
	return result, nil
}
