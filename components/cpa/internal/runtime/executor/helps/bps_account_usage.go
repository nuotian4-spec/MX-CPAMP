package helps

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const BPSAccountUsageURL = "https://chatgpt.com/backend-api/wham/usage"
const BPSAccountUsageUserAgent = "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"

// BPSAccountUsageObservation 只投影官方账号主用量，不定义 BPS 额度桶。
type BPSAccountUsageObservation struct {
	Provider       string               `json:"provider"`
	AuthIndex      string               `json:"auth_index"`
	AuthID         string               `json:"auth_id"`
	Source         string               `json:"source"`
	QueriedAt      string               `json:"queried_at"`
	UpstreamStatus *int                 `json:"upstream_status"`
	Error          *string              `json:"error"`
	PlanType       *string              `json:"plan_type"`
	RateLimit      *BPSAccountRateLimit `json:"rate_limit"`
}

type BPSAccountRateLimit struct {
	Allowed         *bool                  `json:"allowed"`
	LimitReached    *bool                  `json:"limit_reached"`
	PrimaryWindow   *BPSAccountUsageWindow `json:"primary_window"`
	SecondaryWindow *BPSAccountUsageWindow `json:"secondary_window"`
}

type BPSAccountUsageWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *float64 `json:"limit_window_seconds"`
	ResetAfterSeconds  *float64 `json:"reset_after_seconds"`
	ResetAt            *float64 `json:"reset_at"`
}

func (r *BPSAccountUsageObservation) Fail(category string) {
	r.PlanType, r.RateLimit, r.Error = nil, nil, &category
}

// QueryBPSAccountUsage 仅使用精确凭证快照，不进入刷新或推理执行器。
func QueryBPSAccountUsage(parent context.Context, cfg *config.Config, auth *coreauth.Auth) (result BPSAccountUsageObservation) {
	result.Provider, result.Source = "bps", "chatgpt_account_usage"
	defer func() { result.QueriedAt = time.Now().UTC().Format(time.RFC3339) }()
	if auth == nil || auth.Provider != "bps" {
		result.Fail("credential_incomplete")
		return
	}
	result.AuthIndex, result.AuthID = auth.EnsureIndex(), auth.ID
	token, _ := auth.Metadata["access_token"].(string)
	accountID, _ := auth.Metadata["account_id"].(string)
	token, accountID = strings.TrimSpace(token), strings.TrimSpace(accountID)
	if token == "" || accountID == "" || strings.ContainsAny(token+accountID, "\r\n") {
		result.Fail("credential_incomplete")
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, BPSAccountUsageURL, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Chatgpt-Account-Id", accountID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", BPSAccountUsageUserAgent)
	client := NewProxyAwareHTTPClient(ctx, cfg, auth, 0)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		result.Fail(bpsAccessTransportError(ctx, err))
		return
	}
	if response == nil || response.Body == nil {
		result.Fail("invalid_response")
		return
	}
	defer func() { _ = response.Body.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = response.Body.Close() })
	defer stop()
	status := response.StatusCode
	result.UpstreamStatus = &status
	if status != http.StatusOK {
		category := "upstream_error"
		switch {
		case status == http.StatusUnauthorized:
			category = "unauthorized"
		case status == http.StatusForbidden:
			category = "forbidden"
		case status == http.StatusTooManyRequests:
			category = "rate_limited"
		case status >= 300 && status < 400:
			category = "redirect_rejected"
		}
		result.Fail(category)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, bpsAccessBodyLimit+1))
	if err != nil || ctx.Err() != nil {
		result.Fail(bpsAccessTransportError(ctx, err))
		return
	}
	if len(raw) > bpsAccessBodyLimit {
		result.Fail("invalid_response")
		return
	}
	plan, rate, category := parseBPSAccountUsage(raw)
	if category != "" {
		result.Fail(category)
		return
	}
	result.PlanType, result.RateLimit = plan, rate
	return
}

func parseBPSAccountUsage(raw []byte) (*string, *BPSAccountRateLimit, string) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return nil, nil, "invalid_response"
	}
	var plan *string
	if value := root["plan_type"]; len(value) > 0 {
		if err := json.Unmarshal(value, &plan); err != nil {
			return nil, nil, "invalid_response"
		}
	}
	if plan != nil {
		switch *plan {
		case "free", "go", "plus", "prolite", "pro", "team", "business", "self_serve_business", "enterprise", "edu", "education":
		default:
			plan = nil
		}
	}
	var rate *BPSAccountRateLimit
	if value := root["rate_limit"]; len(value) > 0 {
		if err := json.Unmarshal(value, &rate); err != nil {
			return nil, nil, "invalid_response"
		}
	}
	if rate == nil {
		return nil, nil, "quota_unavailable"
	}
	for _, target := range []**BPSAccountUsageWindow{&rate.PrimaryWindow, &rate.SecondaryWindow} {
		window := *target
		if window == nil {
			continue
		}
		for _, number := range []*float64{window.UsedPercent, window.LimitWindowSeconds, window.ResetAfterSeconds, window.ResetAt} {
			if number != nil && (math.IsNaN(*number) || math.IsInf(*number, 0) || *number < 0) {
				return nil, nil, "invalid_response"
			}
		}
		if window.UsedPercent != nil && *window.UsedPercent > 100 || window.LimitWindowSeconds != nil && *window.LimitWindowSeconds <= 0 {
			return nil, nil, "invalid_response"
		}
		if window.UsedPercent == nil && window.ResetAfterSeconds == nil && window.ResetAt == nil {
			*target = nil
		}
	}
	if rate.PrimaryWindow == nil && rate.SecondaryWindow == nil {
		return nil, nil, "quota_unavailable"
	}
	return plan, rate, ""
}
