package cpa

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

// RequestKiloProbes只读运行投影或保存配置，不调用模型生成端点。
func RequestKiloProbes(ctx context.Context, client *http.Client, baseURL, key, method, id string, fields map[string]json.RawMessage) (model.KiloProbes, error) {
	fail := func(status int) (model.KiloProbes, error) { return model.KiloProbes{}, &KiloWriteError{Status: status} }
	var body io.Reader
	if method == http.MethodPut {
		raw, err := json.Marshal(fields)
		if err != nil {
			return fail(400)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, NormalizeBaseURL(baseURL)+"/v8/management/kilo/accounts/"+url.PathEscape(id)+"/probes", body)
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
	if res.StatusCode != 200 {
		switch res.StatusCode {
		case 400, 404, 409, 499, 503:
			return fail(res.StatusCode)
		default:
			return fail(502)
		}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if ctx.Err() != nil {
		return fail(499)
	}
	var result model.KiloProbes
	if err != nil || len(raw) > 1024*1024 || json.Unmarshal(raw, &result) != nil || result.AccountID != id || result.Revision < 0 || result.ManualModelCalls < 0 {
		return fail(502)
	}
	if _, err = time.Parse(time.RFC3339Nano, result.RuntimeStartedAt); err != nil {
		return fail(502)
	}
	if result.Health.Model != "kilo-auto/free" || result.Health.IntervalSeconds != 300 || !validKiloProbeState(result.Health) || !validKiloProbeState(result.ModelProbe) {
		return fail(502)
	}
	result.ConnectionRef = ""
	return result, nil
}

func validKiloProbeState(state model.KiloProbeState) bool {
	if state.IntervalSeconds < 60 || state.IntervalSeconds > 86400 || state.ModelCalls < 0 || state.SkippedCount < 0 || (state.Enabled && strings.TrimSpace(state.Model) == "") {
		return false
	}
	if !slices.Contains([]string{"never", "scheduled", "running", "completed", "incomplete", "failed", "canceled", "skipped"}, state.Status) {
		return false
	}
	if state.ErrorCode != nil && !slices.Contains([]string{"incomplete", "request_failed", "canceled", "account_disabled", "account_restricted", "account_unavailable", "account_busy"}, *state.ErrorCode) {
		return false
	}
	for _, stamp := range []*string{state.NextRunAt, state.LastStartedAt, state.LastCompletedAt} {
		if stamp != nil {
			if _, err := time.Parse(time.RFC3339Nano, *stamp); err != nil {
				return false
			}
		}
	}
	return state.Enabled || state.NextRunAt == nil
}
