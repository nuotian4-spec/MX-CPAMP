package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func FetchBPSUsage(ctx context.Context, client *http.Client, baseURL, key, authIndex, authID string) (model.BPSUsage, error) {
	if strings.TrimSpace(authIndex) == "" {
		return model.BPSUsage{}, fmt.Errorf("missing_auth_index")
	}
	query := url.Values{"auth_index": {authIndex}}
	if authID != "" {
		query.Set("auth_id", authID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeBaseURL(baseURL)+"/v8/management/bps/credentials/usage?"+query.Encode(), nil)
	if err != nil {
		return model.BPSUsage{}, fmt.Errorf("request_error")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	localClient := *client
	localClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := localClient.Do(req)
	if err != nil {
		return model.BPSUsage{}, fmt.Errorf("request_error")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return model.BPSUsage{}, fmt.Errorf("management_http_%d", res.StatusCode)
	}
	var result model.BPSUsage
	if json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&result) != nil || result.Provider != "bps" || result.Source != "chatgpt_account_usage" || result.AuthIndex != authIndex || (authID != "" && result.AuthID != authID) {
		return model.BPSUsage{}, fmt.Errorf("invalid_response")
	}
	if _, err := time.Parse(time.RFC3339, result.QueriedAt); err != nil {
		return model.BPSUsage{}, fmt.Errorf("invalid_response")
	}
	if result.Error != nil {
		switch *result.Error {
		case "credential_incomplete", "unauthorized", "forbidden", "rate_limited", "upstream_error", "redirect_rejected", "invalid_response", "timeout", "canceled", "transport_error", "credential_changed", "quota_unavailable":
		default:
			return model.BPSUsage{}, fmt.Errorf("invalid_response")
		}
		if result.RateLimit != nil || result.PlanType != nil {
			return model.BPSUsage{}, fmt.Errorf("invalid_response")
		}
	} else {
		if result.UpstreamStatus == nil || *result.UpstreamStatus != 200 || result.RateLimit == nil {
			return model.BPSUsage{}, fmt.Errorf("invalid_response")
		}
		for _, w := range []*model.BPSUsageWindow{result.RateLimit.PrimaryWindow, result.RateLimit.SecondaryWindow} {
			if w == nil {
				continue
			}
			for _, n := range []*float64{w.UsedPercent, w.LimitWindowSeconds, w.ResetAfterSeconds, w.ResetAt} {
				if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0) || *n < 0) {
					return model.BPSUsage{}, fmt.Errorf("invalid_response")
				}
			}
			if w.UsedPercent != nil && *w.UsedPercent > 100 {
				return model.BPSUsage{}, fmt.Errorf("invalid_response")
			}
		}
		if len(result.QuotaWindows()) == 0 {
			return model.BPSUsage{}, fmt.Errorf("invalid_response")
		}
	}
	return result, nil
}
