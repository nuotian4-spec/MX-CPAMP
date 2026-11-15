package cpa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

var ErrKiloDeviceEgressUnavailable = errors.New("设备授权出口不可用，授权未启动")

func RequestKiloDevice(ctx context.Context, client *http.Client, baseURL, key, action, id string, fields map[string]json.RawMessage) (model.KiloDeviceResult, error) {
	var result model.KiloDeviceResult
	fail := func(status int) (model.KiloDeviceResult, error) { return result, &KiloWriteError{Status: status} }
	resource, method, want := "device-auth", http.MethodPost, 200
	if action != "start" {
		resource += "/" + url.PathEscape(id)
		if action == "cancel" {
			method = http.MethodDelete
		} else {
			resource += "/" + action
		}
	}
	if action == "save" {
		want = 201
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return fail(400)
	}
	req, err := http.NewRequestWithContext(ctx, method, NormalizeBaseURL(baseURL)+"/v8/management/kilo/"+resource, bytes.NewReader(raw))
	if err != nil {
		return fail(502)
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
			return fail(499)
		}
		return fail(503)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		if action == "start" && res.StatusCode == http.StatusServiceUnavailable {
			var body struct {
				Error string `json:"error"`
			}
			if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&body) == nil && body.Error == "Kilo authorization egress is unavailable" {
				return result, ErrKiloDeviceEgressUnavailable
			}
		}
		switch res.StatusCode {
		case 400, 403, 404, 409, 429, 499, 502, 503:
			return fail(res.StatusCode)
		default:
			return fail(502)
		}
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, 128*1024+1))
	if ctx.Err() != nil {
		return fail(499)
	}
	if err != nil || len(raw) > 128*1024 {
		return fail(502)
	}
	switch action {
	case "start":
		var v model.KiloDeviceStarted
		if json.Unmarshal(raw, &v) != nil || !deviceText(v.ID, 256) || strings.ContainsAny(v.ID, "/\\") || !deviceText(v.UserCode, 128) || !validKiloVerificationURL(v.VerificationURL) || v.ExpiresIn < 1 || v.ExpiresIn > 86400 || v.Interval != 3 || (v.InferenceEgressMode != "pool" && v.InferenceEgressMode != "direct" && v.InferenceEgressMode != "auto") {
			return fail(502)
		}
		v.ConnectionRef = ""
		result.Started = &v
	case "save":
		var v model.KiloDeviceSaved
		if json.Unmarshal(raw, &v) != nil || !validKiloAccountSummary(&v.KiloAccount) || v.ID == "" || v.EffectiveLimit < 1 || v.Active < 0 || v.Waiting < 0 || (v.EgressMode != "pool" && v.EgressMode != "direct" && v.EgressMode != "auto") {
			return fail(502)
		}
		if v.QuotaSource != "none" && v.QuotaSource != "codex_ref" && v.QuotaSource != "imported" {
			return fail(502)
		}
		if v.EgressStatus == "available" {
			if v.EgressReason != "" {
				return fail(502)
			}
		} else if v.EgressStatus == "pending" {
			switch v.EgressReason {
			case "shared_proxy_not_configured", "shared_proxy_unverified", "shared_proxy_unhealthy", "shared_proxy_capacity", "auto_egress_waiting", "auto_egress_draining", "auto_egress_unavailable", "auto_egress_storage_failed", "auto_egress_capacity", "specified_conflict", "capacity_wait", "storage_failed":
			default:
				return fail(502)
			}
		} else {
			return fail(502)
		}
		v.ConnectionRef = ""
		result.Saved = &v
	default:
		var v model.KiloDeviceState
		if json.Unmarshal(raw, &v) != nil {
			return fail(502)
		}
		if action == "cancel" && v.Status != "cancelled" && v.Status != "saved" {
			return fail(502)
		}
		switch v.Status {
		case "approved":
			if v.SaveExpiresIn == nil || *v.SaveExpiresIn < 0 || *v.SaveExpiresIn > 300 || len(v.Email) > 1024 {
				return fail(502)
			}
			v.AccountID = ""
		case "saved":
			if !deviceText(v.AccountID, 256) {
				return fail(502)
			}
			v.Email, v.SaveExpiresIn = "", nil
		case "pending", "denied", "expired", "cancelled":
			v.Email, v.SaveExpiresIn, v.AccountID = "", nil, ""
		default:
			return fail(502)
		}
		v.ConnectionRef = ""
		result.State = &v
	}
	return result, nil
}

func deviceText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validKiloVerificationURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && (strings.ToLower(u.Hostname()) == "kilo.ai" || strings.HasSuffix(strings.ToLower(u.Hostname()), ".kilo.ai")) && len(raw) <= 2048
}
