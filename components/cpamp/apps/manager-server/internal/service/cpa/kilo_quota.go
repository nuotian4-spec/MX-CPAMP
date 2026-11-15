package cpa

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

var quotaSourceStatuses = []string{"ready", "refresh_required", "needs_sign_in", "identity_unknown", "no_outer_material", "unavailable"}
var quotaErrorCodes = []string{"source_missing", "source_changed", "identity_unknown", "no_outer_material", "source_unavailable", "refresh_failed", "refresh_proxy_invalid", "needs_reimport", "usage_unauthorized", "usage_forbidden", "usage_rate_limited", "usage_http_error", "usage_invalid_response", "usage_network_error", "usage_cancelled", "egress_unavailable", "query_egress_unavailable"}
var quotaGroup = regexp.MustCompile(`^additional_rate_limits:[0-9]+$`)

func RequestKiloQuota(ctx context.Context, client *http.Client, baseURL, key, action, id string, fields map[string]json.RawMessage) (model.KiloQuotaResult, error) {
	var result model.KiloQuotaResult
	fail := func(status int) (model.KiloQuotaResult, error) {
		return model.KiloQuotaResult{}, &KiloWriteError{Status: status}
	}
	path, method := "quota-sources", http.MethodGet
	if action != "sources" {
		path = "accounts/" + url.PathEscape(id) + "/quota"
	}
	switch action {
	case "policy_get", "policy_put":
		path = "accounts/" + url.PathEscape(id) + "/source-policy"
		if action == "policy_put" {
			method = http.MethodPut
		}
	case "put":
		path += "-source"
		method = http.MethodPut
	case "delete":
		path += "-source"
		method = http.MethodDelete
	case "query":
		path += "/query"
		method = http.MethodPost
	case "get", "sources":
	default:
		return fail(400)
	}
	var body io.Reader
	if method != http.MethodGet {
		raw, err := json.Marshal(fields)
		if err != nil {
			return fail(400)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, NormalizeBaseURL(baseURL)+"/v8/management/kilo/"+path, body)
	if err != nil {
		return fail(502)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	local := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		local = *client
	}
	transport := local.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if standard, ok := transport.(*http.Transport); ok {
		isolated := standard.Clone()
		isolated.DisableKeepAlives = true
		local.Transport = isolated
		defer isolated.CloseIdleConnections()
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
	if res.StatusCode != 200 && !(action == "query" && res.StatusCode == 499) {
		switch res.StatusCode {
		case 400, 401, 403, 404, 409, 499, 502, 503:
			return fail(res.StatusCode)
		default:
			return fail(502)
		}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 128*1024+1))
	if err != nil || len(raw) > 128*1024 {
		if ctx.Err() != nil {
			return fail(499)
		}
		return fail(502)
	}
	if action == "policy_get" || action == "policy_put" {
		var v model.KiloSourcePolicy
		if json.Unmarshal(raw, &v) != nil || !validKiloSourcePolicy(&v) {
			return fail(502)
		}
		result.Policy = &v
	} else if action == "sources" {
		var v model.KiloQuotaSources
		if json.Unmarshal(raw, &v) != nil || v.Sources == nil {
			return fail(502)
		}
		for _, source := range v.Sources {
			if !deviceText(source.CredentialID, 1024) || !slices.Contains(quotaSourceStatuses, source.Status) || len(source.Label) > 2048 || (source.ChatGPTAccountID != nil && !deviceText(*source.ChatGPTAccountID, 1024)) {
				return fail(502)
			}
		}
		v.ConnectionRef = ""
		result.Sources = &v
	} else {
		var v model.KiloQuotaView
		if json.Unmarshal(raw, &v) != nil || !validQuotaView(raw, &v) || v.AccountID != id {
			return fail(502)
		}
		if res.StatusCode == 499 && v.Query.Status != "cancelled" {
			return fail(502)
		}
		v.ConnectionRef = ""
		result.View = &v
	}
	result.Status = res.StatusCode
	return result, nil
}

func quotaTime(value *string) bool {
	if value == nil {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	_, offset := parsed.Zone()
	return err == nil && offset == 0
}
func validQuotaView(raw []byte, v *model.KiloQuotaView) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return false
	}
	for _, key := range []string{"account_id", "binding_revision", "source", "query", "plan", "windows", "credits", "stale"} {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	if string(object["binding_revision"]) == "null" || string(object["stale"]) == "null" || !deviceText(v.AccountID, 1024) || v.BindingRevision > 9007199254740991 || v.Windows == nil {
		return false
	}
	if !slices.Contains([]string{"none", "cpa_codex", "imported"}, v.Source.Kind) {
		return false
	}
	if !slices.Contains(quotaSourceStatuses, v.Source.Status) && !slices.Contains([]string{"none", "source_missing", "source_changed", "needs_reimport"}, v.Source.Status) {
		return false
	}
	if v.Source.Kind == "cpa_codex" {
		if !deviceText(v.Source.CredentialID, 1024) {
			return false
		}
	} else {
		v.Source.CredentialID = ""
	}
	if !slices.Contains([]string{"never", "running", "success", "failed", "cancelled"}, v.Query.Status) || !quotaTime(v.Query.AttemptedAt) || !quotaTime(v.Query.ObservedAt) {
		return false
	}
	if v.Query.ErrorCode != nil && !slices.Contains(quotaErrorCodes, *v.Query.ErrorCode) {
		return false
	}
	var query, credits map[string]json.RawMessage
	if json.Unmarshal(object["query"], &query) != nil || json.Unmarshal(object["credits"], &credits) != nil {
		return false
	}
	for _, key := range []string{"attempted_at", "observed_at", "error_code"} {
		if _, ok := query[key]; !ok {
			return false
		}
	}
	for _, key := range []string{"remaining", "source_field", "unit"} {
		if _, ok := credits[key]; !ok {
			return false
		}
	}
	for _, window := range v.Windows {
		if window.UsedPercent != nil && (*window.UsedPercent < 0 || math.IsInf(*window.UsedPercent, 0) || math.IsNaN(*window.UsedPercent)) {
			return false
		}
		if window.Group != "rate_limit" && !quotaGroup.MatchString(window.Group) {
			return false
		}
		if !slices.Contains([]string{"primary_window", "secondary_window"}, window.Slot) || window.Source != "api_query" || !quotaTime(window.ResetAt) {
			return false
		}
		if window.DurationSeconds != nil && *window.DurationSeconds <= 0 {
			return false
		}
		if window.RemainingPercent != nil && (*window.RemainingPercent < 0 || *window.RemainingPercent > 100) {
			return false
		}
	}
	if v.Plan != nil && len(*v.Plan) > 256 {
		return false
	}
	if v.Credits.Unit != nil && len(*v.Credits.Unit) > 128 {
		return false
	}
	if v.Credits.SourceField != nil && !slices.Contains([]string{"spend_control.individual_limit.remaining", "spend_control.individual_limit.limit-used", "credits.remaining", "credits.balance"}, *v.Credits.SourceField) {
		return false
	}
	return !strings.Contains(v.AccountID, "\x00")
}
